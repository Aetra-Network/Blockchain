package addressing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const (
	LabelSourceLocal             = "local"
	LabelSourceOnChain           = "on_chain"
	LabelSourceSignedAttestation = "signed_attestation"

	AddressWarningNewAddress        = "new-address"
	AddressWarningRecentlyCreated   = "recently-created"
	AddressWarningPrefixSuffixMatch = "prefix-suffix-collision"
	AddressWarningLookalike         = "address-lookalike"
	AddressWarningConfusableLabel   = "confusable-label"
	AddressWarningUnverifiedLabel   = "unverified-label"

	DefaultRecentAddressWindow = uint64(10_000)
)

type AddressBook struct {
	ChainID string             `json:"chain_id,omitempty"`
	Entries []AddressBookEntry `json:"entries"`
}

type AddressBookEntry struct {
	Address        string `json:"address"`
	Label          string `json:"label,omitempty"`
	LabelSource    string `json:"label_source,omitempty"`
	Attestation    string `json:"attestation,omitempty"`
	CreatedHeight  uint64 `json:"created_height,omitempty"`
	LastSeenHeight uint64 `json:"last_seen_height,omitempty"`
}

type AddressInspectionContext struct {
	ChainID               string
	CurrentHeight         uint64
	RecentAddressWindow   uint64
	AddressBook           []AddressBookEntry
	IncludeSystemContacts bool
}

type AddressDisplay struct {
	Raw                string           `json:"raw"`
	UserFriendly       string           `json:"user_friendly"`
	Short              string           `json:"short"`
	ChainBoundChecksum string           `json:"chain_bound_checksum"`
	Known              bool             `json:"known"`
	LocalLabel         string           `json:"local_label,omitempty"`
	VerifiedLabel      string           `json:"verified_label,omitempty"`
	LabelSource        string           `json:"label_source,omitempty"`
	CreatedHeight      uint64           `json:"created_height,omitempty"`
	LastSeenHeight     uint64           `json:"last_seen_height,omitempty"`
	Warnings           []AddressWarning `json:"warnings,omitempty"`
}

type AddressWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Match   string `json:"match,omitempty"`
}

func NewAddressBookEntry(label string, address string, source string, attestation string, createdHeight uint64, lastSeenHeight uint64) (AddressBookEntry, error) {
	entry := AddressBookEntry{
		Address:        strings.TrimSpace(address),
		Label:          strings.TrimSpace(label),
		LabelSource:    normalizeLabelSource(source),
		Attestation:    strings.TrimSpace(attestation),
		CreatedHeight:  createdHeight,
		LastSeenHeight: lastSeenHeight,
	}
	if err := ValidateAddressBookEntry(entry); err != nil {
		return AddressBookEntry{}, err
	}
	return entry, nil
}

func ValidateAddressBookEntry(entry AddressBookEntry) error {
	if strings.TrimSpace(entry.Address) == "" {
		return fmt.Errorf("address book entry address is required")
	}
	if _, err := Parse(entry.Address); err != nil {
		return fmt.Errorf("address book entry address invalid: %w", err)
	}
	if strings.TrimSpace(entry.Label) == "" {
		return fmt.Errorf("address book entry label is required")
	}
	source := normalizeLabelSource(entry.LabelSource)
	switch source {
	case LabelSourceLocal, LabelSourceOnChain:
	case LabelSourceSignedAttestation:
		if strings.TrimSpace(entry.Attestation) == "" {
			return fmt.Errorf("signed address label requires an attestation")
		}
	default:
		return fmt.Errorf("unsupported address label source %q", entry.LabelSource)
	}
	if entry.LastSeenHeight != 0 && entry.CreatedHeight != 0 && entry.LastSeenHeight < entry.CreatedHeight {
		return fmt.Errorf("address book last seen height cannot precede creation height")
	}
	return nil
}

func InspectAddress(text string, ctx AddressInspectionContext) (AddressDisplay, error) {
	bz, err := Parse(text)
	if err != nil {
		return AddressDisplay{}, err
	}
	raw := Format(bz)
	userFriendly, err := FormatUserFriendly(bz)
	if err != nil {
		return AddressDisplay{}, err
	}
	if system, found := SystemAddressByBytes(bz); found {
		raw = system.Raw
		userFriendly = system.UserFriendly
	}
	out := AddressDisplay{
		Raw:                raw,
		UserFriendly:       userFriendly,
		Short:              ShortenAddress(userFriendly),
		ChainBoundChecksum: ChainBoundChecksum(ctx.ChainID, raw),
	}

	contacts := inspectionContacts(ctx)
	targetKey, err := addressBytesKey(bz)
	if err != nil {
		return AddressDisplay{}, err
	}
	for _, contact := range contacts {
		contactKey, err := addressTextKey(contact.Address)
		if err != nil {
			continue
		}
		if contactKey != targetKey {
			continue
		}
		out.Known = true
		out.CreatedHeight = contact.CreatedHeight
		out.LastSeenHeight = contact.LastSeenHeight
		source := normalizeLabelSource(contact.LabelSource)
		if contact.Label != "" {
			out.LocalLabel = contact.Label
			out.LabelSource = source
			if isVerifiedLabelSource(source, contact.Attestation) {
				out.VerifiedLabel = contact.Label
			} else {
				out.Warnings = append(out.Warnings, AddressWarning{
					Code:    AddressWarningUnverifiedLabel,
					Message: "label is local-only and must not be shown as verified identity",
				})
			}
		}
	}
	if !out.Known {
		out.Warnings = append(out.Warnings, AddressWarning{
			Code:    AddressWarningNewAddress,
			Message: "recipient is not in the address book or verified system catalog",
		})
	}
	if out.CreatedHeight != 0 && ctx.CurrentHeight != 0 {
		window := ctx.RecentAddressWindow
		if window == 0 {
			window = DefaultRecentAddressWindow
		}
		if ctx.CurrentHeight >= out.CreatedHeight && ctx.CurrentHeight-out.CreatedHeight <= window {
			out.Warnings = append(out.Warnings, AddressWarning{
				Code:    AddressWarningRecentlyCreated,
				Message: fmt.Sprintf("address was created %d blocks ago", ctx.CurrentHeight-out.CreatedHeight),
			})
		}
	}
	out.Warnings = append(out.Warnings, similarAddressWarnings(out, contacts, targetKey)...)
	return out, nil
}

func ShortenAddress(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 22 {
		return text
	}
	return text[:10] + "..." + text[len(text)-8:]
}

func ChainBoundChecksum(chainID string, address string) string {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		chainID = "unknown-chain"
	}
	key, err := addressTextKey(address)
	if err != nil {
		key = strings.TrimSpace(address)
	}
	sum := sha256.Sum256([]byte(chainID + "|" + key))
	return chainID + ":" + hex.EncodeToString(sum[:4])
}

func isVerifiedLabelSource(source string, attestation string) bool {
	source = normalizeLabelSource(source)
	return source == LabelSourceOnChain || (source == LabelSourceSignedAttestation && strings.TrimSpace(attestation) != "")
}

func normalizeLabelSource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return LabelSourceLocal
	}
	return source
}

func inspectionContacts(ctx AddressInspectionContext) []AddressBookEntry {
	contacts := make([]AddressBookEntry, 0, len(ctx.AddressBook)+len(reservedSystemAddresses))
	if ctx.IncludeSystemContacts {
		for _, system := range reservedSystemAddresses {
			contacts = append(contacts, AddressBookEntry{
				Address:     system.UserFriendly,
				Label:       system.Name,
				LabelSource: LabelSourceOnChain,
			})
		}
	}
	for _, entry := range ctx.AddressBook {
		if ValidateAddressBookEntry(entry) == nil {
			entry.LabelSource = normalizeLabelSource(entry.LabelSource)
			contacts = append(contacts, entry)
		}
	}
	return contacts
}

func similarAddressWarnings(target AddressDisplay, contacts []AddressBookEntry, targetKey string) []AddressWarning {
	var warnings []AddressWarning
	targetShort := ShortenAddress(target.UserFriendly)
	targetSkeleton := visualSkeleton(target.UserFriendly)
	targetLabelSkeleton := visualSkeleton(firstNonEmpty(target.VerifiedLabel, target.LocalLabel))
	for _, contact := range contacts {
		contactKey, err := addressTextKey(contact.Address)
		if err != nil || contactKey == targetKey {
			continue
		}
		contactBytes, err := Parse(contact.Address)
		if err != nil {
			continue
		}
		contactUser, err := FormatUserFriendly(contactBytes)
		if err != nil {
			continue
		}
		match := firstNonEmpty(contact.Label, ShortenAddress(contactUser))
		if ShortenAddress(contactUser) == targetShort {
			warnings = append(warnings, AddressWarning{
				Code:    AddressWarningPrefixSuffixMatch,
				Message: "address short form collides with another known address",
				Match:   match,
			})
		}
		if boundedEditDistance(targetSkeleton, visualSkeleton(contactUser), 2) <= 2 {
			warnings = append(warnings, AddressWarning{
				Code:    AddressWarningLookalike,
				Message: "address is visually close to another known address",
				Match:   match,
			})
		}
		contactLabelSkeleton := visualSkeleton(contact.Label)
		if targetLabelSkeleton != "" && contactLabelSkeleton != "" && targetLabelSkeleton == contactLabelSkeleton {
			warnings = append(warnings, AddressWarning{
				Code:    AddressWarningConfusableLabel,
				Message: "address label is visually confusable with another known label",
				Match:   match,
			})
		}
	}
	return warnings
}

func visualSkeleton(text string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(text) {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		r = unicode.ToLower(r)
		switch r {
		case '0', '\u03bf', '\u043e':
			r = 'o'
		case '1', 'i', '\u0131', '\u0456', '\u04cf':
			r = 'l'
		case '3':
			r = 'e'
		case '5':
			r = 's'
		case '@', '\u0430':
			r = 'a'
		case '$':
			r = 's'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func boundedEditDistance(a string, b string, max int) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if abs(len(a)-len(b)) > max {
		return max + 1
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minInt(minInt(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > max {
			return max + 1
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
