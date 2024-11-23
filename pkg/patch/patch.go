package patch

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.uber.org/multierr"
)

type Provider string

const (
	ProviderUnknown Provider = "unknown"
)

var (
	ErrNotExist = os.ErrNotExist
)

type Patchable interface {
	GetFileName() string
	GetFingerprints() map[Provider]Fingerprint
	GetModifications(old, new Provider) ([]Modification, error)
}

type Fingerprint interface {
	Matches(b []byte) bool
}

type Modification struct {
	Old    []byte
	New    []byte
	Length int
	Count  int
}

func Patch(patchable Patchable, dir string, new Provider) (err error) {
	path := filepath.Join(dir, patchable.GetFileName())

	stats, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotExist
		}
		return err
	}

	f, err := os.OpenFile(path, os.O_RDWR, stats.Mode())
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotExist
		}
		return err
	}
	defer multierr.AppendInvoke(&err, multierr.Close(f))

	original, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	// Detect "old"/current provider based on what's in the binary
	old, err := determineCurrentlyUsedProvider(original, patchable.GetFingerprints())
	if err != nil {
		return err
	}

	// No need to patch if binary is already patched as desired
	if new == old {
		return nil
	}

	modifications, err := patchable.GetModifications(old, new)
	if err != nil {
		return err
	}

	// Apply modifications to a copy of the original
	modified := original[:]
	for _, m := range modifications {
		o := padRight(m.Old, 0, m.Length)
		n := padRight(m.New, 0, m.Length)

		count := bytes.Count(modified, o)
		if count != m.Count {
			return fmt.Errorf("binary contains unknown modifications, revert changes first")
		}

		// Replace all occurrences, making sure to keep the binary the same length
		modified = bytes.ReplaceAll(modified, o, n)
	}

	// Any changes to the length would break the binary
	if len(modified) != len(original) {
		return fmt.Errorf("length of modified binary does not match length of original")
	}

	_, err = f.WriteAt(modified, 0)
	if err != nil {
		return err
	}

	return nil
}

func determineCurrentlyUsedProvider(b []byte, fingerprints map[Provider]Fingerprint) (Provider, error) {
	for provider, fingerprint := range fingerprints {
		if fingerprint.Matches(b) {
			return provider, nil
		}
	}

	return ProviderUnknown, fmt.Errorf("binary contains unknown/mixed modifications, revert changes first")
}

func padRight(b []byte, c byte, l int) []byte {
	if len(b) >= l {
		return b
	}

	p := make([]byte, len(b), l)
	copy(p, b)
	for len(p) < l {
		p = append(p, c)
	}

	return p
}

func ContainsAll(b []byte, bbs [][]byte) bool {
	for _, bb := range bbs {
		if !bytes.Contains(b, bb) {
			return false
		}
	}

	return true
}
