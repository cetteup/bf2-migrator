package patchable

import (
	"fmt"

	"github.com/cetteup/bf2-migrator/pkg/patch"
)

const (
	ServerExecutableName = "bf2_w32ded.exe"
)

type ServerExecutable struct{}

func (e ServerExecutable) GetFileName() string {
	return ServerExecutableName
}

func (e ServerExecutable) GetFingerprints() map[patch.Provider]patch.Fingerprint {
	efs := e.getFingerprints()
	pfs := make(map[patch.Provider]patch.Fingerprint, len(efs))
	for provider, fingerprint := range efs {
		pfs[provider] = fingerprint
	}

	return pfs
}

func (e ServerExecutable) GetModifications(old, new patch.Provider) ([]patch.Modification, error) {
	fingerprints := e.getFingerprints()

	wipe, ok := fingerprints[old]
	if !ok {
		return nil, fmt.Errorf("missing fingerprint for old provider: %s", old)
	}

	apply, ok := fingerprints[new]
	if !ok {
		return nil, fmt.Errorf("missing fingerprint for new provider: %s", old)
	}

	return []patch.Modification{
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
			Old:    wipe.DLLName,
			New:    apply.DLLName,
			Length: 10,
			Count:  1,
		},
	}, nil
}

func (e ServerExecutable) getFingerprints() map[patch.Provider]serverExecutableFingerprint {
	return map[patch.Provider]serverExecutableFingerprint{
		ProviderBF2Hub: {
			// BF2Hub does not modify the hostname, so modify based on the GameSpy hostname
			Hostname: []byte("gamespy.com"),
			DLLName:  []byte("bf2hub.dll"),
		},
		ProviderPlayBF2: {
			Hostname: []byte("playbf2.ru"),
			DLLName:  []byte("WS2_32.dll"),
		},
		ProviderOpenSpy: {
			Hostname: []byte("openspy.net"),
			DLLName:  []byte("WS2_32.dll"),
		},
		ProviderGameSpy: {
			Hostname: []byte("gamespy.com"),
			DLLName:  []byte("WS2_32.dll"),
		},
	}
}

type serverExecutableFingerprint struct {
	Hostname []byte
	// Part of the "normal" fingerprint here,
	// otherwise a GameSpy-patched binary might be detected as BF2Hub-patched
	DLLName []byte
}

func (f serverExecutableFingerprint) Matches(b []byte) bool {
	ridges := [][]byte{f.Hostname, f.DLLName}
	return patch.ContainsAll(b, ridges)
}
