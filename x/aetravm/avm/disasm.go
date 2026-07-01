package avm

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type DisassemblyLine struct {
	Offset int
	Op     string
	Arg    uint64
	Data   string
}

type GasProfileLine struct {
	Offset int
	Op     string
	Gas    uint64
}

type GasProfile struct {
	Total uint64
	Lines []GasProfileLine
}

func OpcodeName(op Opcode) string {
	switch op {
	case OpNop:
		return "nop"
	case OpPushU64:
		return "push_u64"
	case OpReadStorage:
		return "read_storage"
	case OpWriteStorage:
		return "write_storage"
	case OpAdd:
		return "add"
	case OpEmitInternal:
		return "emit_internal"
	case OpReturn:
		return "return"
	case OpReadMsgOpcode:
		return "read_msg_opcode"
	case OpReadMsgQueryID:
		return "read_msg_query_id"
	case OpReadBlock:
		return "read_block"
	case OpChargeGas:
		return "charge_gas"
	case OpScheduleSelf:
		return "schedule_self"
	case OpWallClock:
		return "wall_clock"
	case OpRandom:
		return "random"
	case OpFileRead:
		return "file_read"
	case OpFloatAdd:
		return "float_add"
	case OpIterMap:
		return "iter_map"
	default:
		return fmt.Sprintf("opcode_0x%02x", byte(op))
	}
}

func DisassembleModule(module Module) []DisassemblyLine {
	out := make([]DisassemblyLine, 0, len(module.Code))
	for i, ins := range module.Code {
		line := DisassemblyLine{
			Offset: i,
			Op:     OpcodeName(ins.Op),
			Arg:    ins.Arg,
			Data:   hex.EncodeToString(ins.Data),
		}
		out = append(out, line)
	}
	return out
}

func FormatDisassembly(module Module) string {
	lines := DisassembleModule(module)
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(fmt.Sprintf("%04d  %-20s", line.Offset, line.Op))
		if line.Arg != 0 {
			b.WriteString(fmt.Sprintf(" arg=%d", line.Arg))
		}
		if line.Data != "" {
			b.WriteString(" data=0x")
			b.WriteString(line.Data)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ProfileGas(module Module, schedule map[Opcode]uint64) GasProfile {
	lines := make([]GasProfileLine, 0, len(module.Code))
	var total uint64
	for i, ins := range module.Code {
		gas := schedule[ins.Op]
		total += gas
		lines = append(lines, GasProfileLine{
			Offset: i,
			Op:     OpcodeName(ins.Op),
			Gas:    gas,
		})
	}
	return GasProfile{Total: total, Lines: lines}
}

func (p GasProfile) SortedLines() []GasProfileLine {
	lines := append([]GasProfileLine(nil), p.Lines...)
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Gas != lines[j].Gas {
			return lines[i].Gas > lines[j].Gas
		}
		return lines[i].Offset < lines[j].Offset
	})
	return lines
}

