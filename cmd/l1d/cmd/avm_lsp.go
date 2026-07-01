package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

type lspMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type lspResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *lspError       `json:"error,omitempty"`
}

type lspError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type lspPublishDiagnostics struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type lspServer struct {
	in   *bufio.Reader
	out  *bufio.Writer
	docs map[string]string
}

func runAVMLanguageServer() error {
	srv := &lspServer{
		in:   bufio.NewReader(os.Stdin),
		out:  bufio.NewWriter(os.Stdout),
		docs: map[string]string{},
	}
	return srv.serve()
}

func (s *lspServer) serve() error {
	for {
		msg, err := s.readMessage()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch msg.Method {
		case "initialize":
			if msg.ID == nil {
				continue
			}
			result := map[string]any{
				"capabilities": map[string]any{
					"textDocumentSync": 1,
					"diagnosticProvider": map[string]any{
						"interFileDependencies": false,
						"workspaceDiagnostics":   false,
					},
				},
			}
			if err := s.writeResponse(*msg.ID, result, nil); err != nil {
				return err
			}
		case "initialized":
			continue
		case "shutdown":
			if msg.ID != nil {
				if err := s.writeResponse(*msg.ID, nil, nil); err != nil {
					return err
				}
			}
			return s.out.Flush()
		case "exit":
			return s.out.Flush()
		case "textDocument/didOpen":
			if err := s.handleDidOpen(msg.Params); err != nil {
				return err
			}
		case "textDocument/didChange":
			if err := s.handleDidChange(msg.Params); err != nil {
				return err
			}
		}
	}
}

func (s *lspServer) handleDidOpen(params json.RawMessage) error {
	var payload struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return err
	}
	s.docs[payload.TextDocument.URI] = payload.TextDocument.Text
	return s.publishDiagnostics(payload.TextDocument.URI, payload.TextDocument.Text)
}

func (s *lspServer) handleDidChange(params json.RawMessage) error {
	var payload struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return err
	}
	if len(payload.ContentChanges) == 0 {
		return nil
	}
	text := payload.ContentChanges[len(payload.ContentChanges)-1].Text
	s.docs[payload.TextDocument.URI] = text
	return s.publishDiagnostics(payload.TextDocument.URI, text)
}

func (s *lspServer) publishDiagnostics(uri string, text string) error {
	diags := compileDiagnostics(uri, text)
	msg := lspPublishDiagnostics{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params: map[string]any{
			"uri":         uri,
			"diagnostics": diags,
		},
	}
	return s.writeNotification(msg)
}

func compileDiagnostics(uri string, text string) []map[string]any {
	res, err := compiler.New(compiler.DefaultOptions())
	if err != nil {
		return []map[string]any{{"severity": 1, "message": err.Error()}}
	}
	_, err = res.Compile([]byte(text))
	if err == nil {
		return nil
	}
	if cErr, ok := err.(*compiler.CompileError); ok {
		out := make([]map[string]any, 0, len(cErr.Diagnostics))
		for _, diag := range cErr.Diagnostics {
			out = append(out, map[string]any{
				"severity": diagSeverityNumber(diag.Severity),
				"code":     diag.Code,
				"message":  diag.Message,
				"source":   "avm",
				"data":     map[string]any{"uri": uri},
				"range": map[string]any{
					"start": map[string]any{"line": max(diag.Pos.Line-1, 0), "character": max(diag.Pos.Column-1, 0)},
					"end":   map[string]any{"line": max(diag.Pos.Line-1, 0), "character": max(diag.Pos.Column, 0)},
				},
			})
		}
		return out
	}
	return []map[string]any{{"severity": 1, "message": err.Error(), "source": "avm", "data": map[string]any{"uri": uri}}}
}

func (s *lspServer) writeResponse(id json.RawMessage, result any, err *lspError) error {
	resp := lspResponse{JSONRPC: "2.0", ID: id, Result: result, Error: err}
	return s.writeMessage(resp)
}

func (s *lspServer) writeNotification(msg any) error {
	return s.writeMessage(msg)
}

func (s *lspServer) writeMessage(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	if _, err := s.out.Write(data); err != nil {
		return err
	}
	return s.out.Flush()
}

func (s *lspServer) readMessage() (lspMessage, error) {
	var contentLength int
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return lspMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			value := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			fmt.Sscanf(value, "%d", &contentLength)
		}
	}
	if contentLength <= 0 {
		return lspMessage{}, io.EOF
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return lspMessage{}, err
	}
	var msg lspMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return lspMessage{}, err
	}
	return msg, nil
}

func diagSeverityNumber(severity compiler.DiagnosticSeverity) int {
	switch severity {
	case compiler.SeverityError:
		return 1
	case compiler.SeverityWarning:
		return 2
	default:
		return 1
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
