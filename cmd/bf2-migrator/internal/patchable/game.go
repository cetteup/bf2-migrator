package patchable

import (
	"fmt"

	"github.com/cetteup/bf2-migrator/pkg/patch"
)

const (
	GameExecutableName = "BF2.exe"
)

type GameExecutable struct{}

func (e GameExecutable) GetFileName() string {
	return GameExecutableName
}

func (e GameExecutable) GetFingerprints() map[patch.Provider]patch.Fingerprint {
	efs := e.getFingerprints()
	pfs := make(map[patch.Provider]patch.Fingerprint, len(efs))
	for provider, fingerprint := range efs {
		pfs[provider] = fingerprint
	}

	return pfs
}

func (e GameExecutable) GetModifications(old, new patch.Provider) ([]patch.Modification, error) {
	fingerprints := e.getFingerprints()

	wipe, ok := fingerprints[old]
	if !ok {
		return nil, fmt.Errorf("missing fingerprint for old provider: %s", old)
	}

	apply, ok := fingerprints[new]
	if !ok {
		return nil, fmt.Errorf("missing fingerprint for new provider: %s", old)
	}

	// Default modifications, required for patching any provider
	modifications := []patch.Modification{
		{
			Old:    wipe.HostsPath,
			New:    apply.HostsPath,
			Length: 18,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gamestats.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("gamestats.%s", apply.Hostname)),
			Length: 21,
			Count:  2,
		},
		{
			Old:    []byte(fmt.Sprintf("http://stage-net.%s/bf2/getplayerinfo.aspx?pid=", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("http://stage-net.%s/bf2/getplayerinfo.aspx?pid=", apply.Hostname)),
			Length: 56,
			Count:  1,
		},
		{
			Old: []byte(fmt.Sprintf("BF2Web.%s", wipe.Hostname)),
			New: []byte(fmt.Sprintf("BF2Web.%s", apply.Hostname)),
			// Actual length of original is 18. However, "BF2Web.%s" would also match the below modification
			// and break the url, so add another trailing nil-byte to avoid the partial match
			Length: 19,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("http://BF2Web.%s/ASP/", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("http://BF2Web.%s/ASP/", apply.Hostname)),
			Length: 30,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("%%s.available.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.available.%s", apply.Hostname)),
			Length: 24,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("%%s.master.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.master.%s", apply.Hostname)),
			Length: 21,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gpcm.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("gpcm.%s", apply.Hostname)),
			Length: 16,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gpsp.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("gpsp.%s", apply.Hostname)),
			Length: 16,
			Count:  1,
		},
	}

	// Semi backend-specific modifications (common for some backends)
	// Special case for PlayBF2: They remove the numeric placeholder/verb ("%d") in addition to changing the hostname
	if old == ProviderPlayBF2 {
		// Remove "%d" when currently patched for PlayBF2
		modifications = append(modifications, patch.Modification{
			Old:    []byte(fmt.Sprintf("%%s.ms.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms%%d.%s", apply.Hostname)),
			Length: 19,
			Count:  1,
		})
	} else if new == ProviderPlayBF2 {
		// Add "%d" when patching to PlayBF2
		modifications = append(modifications, patch.Modification{
			Old:    []byte(fmt.Sprintf("%%s.ms%%d.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms.%s", apply.Hostname)),
			Length: 19,
			Count:  1,
		})
	} else {
		// Symmetrical change for all other providers
		modifications = append(modifications, patch.Modification{
			Old:    []byte(fmt.Sprintf("%%s.ms%%d.%s", wipe.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms%%d.%s", apply.Hostname)),
			Length: 19,
			Count:  1,
		})
	}

	// Truly backend-specific modifications (unique to a single backend to be applied/reverted)
	switch old {
	case ProviderBF2Hub:
		modifications = append(modifications, patch.Modification{
			Old:    []byte("bf2hbc.dll"),
			New:    []byte("WS2_32.dll"),
			Length: 10,
			Count:  1,
		},
		)
	}

	switch new {
	case ProviderBF2Hub:
		modifications = append(modifications, patch.Modification{
			Old:    []byte("WS2_32.dll"),
			New:    []byte("bf2hbc.dll"),
			Length: 10,
			Count:  1,
		},
		)
	}

	return modifications, nil
}

func (e GameExecutable) getFingerprints() map[patch.Provider]gameExecutableFingerprint {
	return map[patch.Provider]gameExecutableFingerprint{
		ProviderBF2Hub: {
			// BF2Hub does not modify the hostname, so modify based on the GameSpy hostname
			Hostname:  []byte("gamespy.com"),
			HostsPath: []byte("\\drivers\\xtc\\hosts"),
			Additional: [][]byte{
				[]byte("bf2hbc.dll"),
			},
		},
		ProviderPlayBF2: {
			Hostname:  []byte("playbf2.ru"),
			HostsPath: []byte("\\drivers\\etc\\hasts"),
		},
		ProviderOpenSpy: {
			Hostname:  []byte("openspy.net"),
			HostsPath: []byte("\\drivers\\etz\\hosts"),
		},
		ProviderGameSpy: {
			Hostname:  []byte("gamespy.com"),
			HostsPath: []byte("\\drivers\\etc\\hosts"),
		},
	}
}

type gameExecutableFingerprint struct {
	Hostname   []byte
	HostsPath  []byte
	Additional [][]byte
}

func (f gameExecutableFingerprint) Matches(b []byte) bool {
	ridges := append(f.Additional, f.Hostname, f.HostsPath)
	return patch.ContainsAll(b, ridges)
}
