package gui

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cetteup/conman/pkg/game/bf2"
	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/mitchellh/go-ps"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"

	"github.com/cetteup/conman/pkg/game"
	"github.com/cetteup/joinme.click-launcher/pkg/software_finder"

	api "github.com/dogclan/bf2-migrator/pkg/openspy"
)

const (
	windowWidth  = 250
	windowHeight = 290

	bf2ExecutableName    = "BF2.exe"
	bf2hubExecutableName = "bf2hub.exe"
)

type provider struct {
	Name        string
	Fingerprint fingerprint
}

type fingerprint struct {
	Hostname   []byte
	HostsPath  []byte
	Additional [][]byte
}

var bf2hub = provider{
	Name: "BF2Hub",
	Fingerprint: fingerprint{
		// BF2Hub does not modify the hostname, so modify based on the GameSpy hostname
		Hostname:  []byte("gamespy.com"),
		HostsPath: []byte("\\drivers\\xtc\\hosts"),
		Additional: [][]byte{
			[]byte("bf2hbc.dll"),
		},
	},
}
var playbf2 = provider{
	Name: "PlayBF2",
	Fingerprint: fingerprint{
		Hostname:  []byte("playbf2.ru"),
		HostsPath: []byte("\\drivers\\etc\\hasts"),
	},
}
var openspy = provider{
	Name: "OpenSpy",
	Fingerprint: fingerprint{
		Hostname:  []byte("openspy.net"),
		HostsPath: []byte("\\drivers\\etz\\hosts"),
	},
}
var gamespy = provider{
	Name: "GameSpy",
	Fingerprint: fingerprint{
		Hostname:  []byte("gamespy.com"),
		HostsPath: []byte("\\drivers\\etc\\hosts"),
	},
}

type client interface {
	CreateAccount(email, password string, partnerCode int) error
	CreateProfile(nick string, namespaceID int) error
	GetProfiles() ([]api.ProfileDTO, error)
}

type finder interface {
	GetInstallDirFromSomewhere(configs []software_finder.Config) (string, error)
}

type registryRepository interface {
	OpenKey(k registry.Key, path string, access uint32, cb func(key registry.Key) error) error
}

func CreateMainWindow(h game.Handler, c client, f finder, r registryRepository) (*walk.MainWindow, error) {
	icon, err := walk.NewIconFromResourceIdWithSize(2, walk.Size{Width: 256, Height: 256})
	if err != nil {
		return nil, err
	}

	screenWidth := win.GetSystemMetrics(win.SM_CXSCREEN)
	screenHeight := win.GetSystemMetrics(win.SM_CYSCREEN)

	var mw *walk.MainWindow
	var profileCB *walk.ComboBox
	var migratePB *walk.PushButton
	var providerCB *walk.ComboBox
	var patchPB *walk.PushButton
	var revertPB *walk.PushButton

	if err := (declarative.MainWindow{
		AssignTo: &mw,
		Title:    "BF2 migrator",
		Name:     "BF2 migrator",
		Bounds: declarative.Rectangle{
			X:      int((screenWidth - windowWidth) / 2),
			Y:      int((screenHeight - windowHeight) / 2),
			Width:  windowWidth,
			Height: windowHeight,
		},
		Layout:  declarative.VBox{},
		Icon:    icon,
		ToolBar: declarative.ToolBar{},
		Children: []declarative.Widget{
			declarative.Label{
				Text:       "Select profile",
				TextColor:  walk.Color(win.GetSysColor(win.COLOR_CAPTIONTEXT)),
				Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
			},
			declarative.ComboBox{
				AssignTo:      &profileCB,
				DisplayMember: "Name",
				BindingMember: "Key",
				Name:          "Select profile",
				ToolTipText:   "Select profile",
				OnCurrentIndexChanged: func() {
					// Password actions cannot be used with singleplayer profiles, since those don't have passwords
					if profileCB.Model().([]game.Profile)[profileCB.CurrentIndex()].Type == game.ProfileTypeMultiplayer {
						migratePB.SetEnabled(true)
					} else {
						migratePB.SetEnabled(false)
					}
				},
			},
			declarative.GroupBox{
				Title:  "Profile actions",
				Name:   "Profile actions",
				Layout: declarative.VBox{},
				Children: []declarative.Widget{
					declarative.PushButton{
						AssignTo: &migratePB,
						Text:     "Migrate to OpenSpy",
						OnClicked: func() {
							// Block any actions during migrations
							mw.SetEnabled(false)
							_ = migratePB.SetText("Migrating...")
							defer func() {
								_ = migratePB.SetText("Migrate to OpenSpy")
								mw.SetEnabled(true)
							}()

							profile := profileCB.Model().([]game.Profile)[profileCB.CurrentIndex()]
							err2 := migrateProfile(h, c, profile.Key)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to migrate %q to OpenSpy: %s", profile.Name, err2.Error()), walk.MsgBoxIconError)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Migrated %q to OpenSpy", profile.Name), walk.MsgBoxIconInformation)
							}
						},
					},
				},
			},
			declarative.Label{
				Text:       "Select provider",
				TextColor:  walk.Color(win.GetSysColor(win.COLOR_CAPTIONTEXT)),
				Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
			},
			declarative.ComboBox{
				AssignTo:      &providerCB,
				DisplayMember: "Name",
				BindingMember: "Name",
				Name:          "Select provider",
				ToolTipText:   "Select provider",
				Model: []provider{
					// Not offering BF2Hub (needs a .dll in addition to .exe changes)
					playbf2,
					openspy,
					// Not offering GameSpy (obsolete, only used for reverting)
				},
				CurrentIndex: 1, // Select OpenSpy as default
			},
			declarative.GroupBox{
				Title:  "Provider actions",
				Name:   "Provider actions",
				Layout: declarative.VBox{},
				Children: []declarative.Widget{
					declarative.PushButton{
						AssignTo: &patchPB,
						Text:     "Apply patch",
						OnClicked: func() {
							// Block any actions during patching
							mw.SetEnabled(false)
							_ = patchPB.SetText("Patching...")
							defer func() {
								_ = patchPB.SetText("Apply patch")
								mw.SetEnabled(true)
							}()

							err2 := prepareForPatch(r)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to prepare for patching %s: %s", bf2ExecutableName, err2.Error()), walk.MsgBoxIconError)
								return
							}

							p := providerCB.Model().([]provider)[providerCB.CurrentIndex()]
							err2 = patchBinary(f, p)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch %s: %s", bf2ExecutableName, err2.Error()), walk.MsgBoxIconError)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Patched %s to use %s", bf2ExecutableName, p.Name), walk.MsgBoxIconInformation)
							}
						},
					},
					declarative.PushButton{
						AssignTo: &revertPB,
						Text:     "Revert patch",
						OnClicked: func() {
							// Block any actions during patching
							mw.SetEnabled(false)
							_ = revertPB.SetText("Reverting...")
							defer func() {
								_ = revertPB.SetText("Revert patch")
								mw.SetEnabled(true)
							}()

							err2 := prepareForPatch(r)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to prepare for reverting %s: %s", bf2ExecutableName, err2.Error()), walk.MsgBoxIconError)
								return
							}

							err2 = patchBinary(f, gamespy)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch %s: %s", bf2ExecutableName, err2.Error()), walk.MsgBoxIconError)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Reverted %s to use GameSpy\n\nYou can now use provider-specific patchers again (e.g. BF2Hub Patcher)", bf2ExecutableName), walk.MsgBoxIconInformation)
							}
						},
					},
				},
			},
			declarative.Label{
				Text:       "BF2 migrator v0.4.0",
				Alignment:  declarative.AlignHCenterVCenter,
				TextColor:  walk.Color(win.GetSysColor(win.COLOR_GRAYTEXT)),
				Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
			},
		},
	}).Create(); err != nil {
		return nil, err
	}

	// Disable minimize/maximize buttons and fix size
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, win.GetWindowLong(mw.Handle(), win.GWL_STYLE) & ^win.WS_MINIMIZEBOX & ^win.WS_MAXIMIZEBOX & ^win.WS_SIZEBOX)

	profiles, selected, err := getProfiles(h)
	if err != nil {
		walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to load list of available profiles: %s", err.Error()), walk.MsgBoxIconError)
		return nil, err
	}
	_ = profileCB.SetModel(profiles)
	_ = profileCB.SetCurrentIndex(selected)

	return mw, nil
}

func getProfiles(h game.Handler) ([]game.Profile, int, error) {
	profiles, err := bf2.GetProfiles(h)
	if err != nil {
		return nil, 0, err
	}

	defaultProfileKey, err := bf2.GetDefaultProfileKey(h)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to get default profile key")
		// If determining the default profile fails, simply pre-select the first profile (don't return an error)
		return profiles, 0, nil
	}

	for i, profile := range profiles {
		if profile.Key == defaultProfileKey {
			return profiles, i, nil
		}
	}

	return profiles, 0, nil
}

func migrateProfile(h game.Handler, c client, profileKey string) error {
	profileCon, err := bf2.ReadProfileConfigFile(h, profileKey, bf2.ProfileConfigFileProfileCon)
	if err != nil {
		return fmt.Errorf("failed to read profile config file: %w", err)
	}

	nick, encrypted, err := bf2.GetEncryptedLogin(profileCon)
	if err != nil {
		return fmt.Errorf("failed to get encrypted login from profile config file: %w", err)
	}

	password, err := bf2.DecryptProfileConPassword(encrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt profile password: %w", err)
	}

	email, err := profileCon.GetValue(bf2.ProfileConKeyEmail)
	if err != nil {
		return fmt.Errorf("failed to get email address from profile config file: %w", err)
	}

	err = c.CreateAccount(email.String(), password, 0)
	if err != nil {
		return fmt.Errorf("failed to create OpenSpy account: %w", err)
	}

	profiles, err := c.GetProfiles()
	if err != nil {
		return fmt.Errorf("failed to get OpenSpy account profiles: %w", err)
	}

	// Don't use slices package here to maintain compatibility with go 1.20 (and thus Windows 7)
	exists := false
	for _, profile := range profiles {
		if profile.UniqueNick == nick && profile.NamespaceID == 12 {
			exists = true
			break
		}
	}

	if !exists {
		err2 := c.CreateProfile(nick, 12)
		if err2 != nil {
			return fmt.Errorf("failed to create OpenSpy profile: %w", err2)
		}
	}

	return nil
}

func prepareForPatch(r registryRepository) error {
	processes, err := ps.Processes()
	if err != nil {
		return fmt.Errorf("failed to retrieve process list: %s", err)
	}

	killed := map[int]string{}
	for _, process := range processes {
		executable := process.Executable()
		if executable == bf2ExecutableName || executable == bf2hubExecutableName {
			pid := process.Pid()
			if err = killProcess(pid); err != nil {
				return fmt.Errorf("failed to kill process %q: %s", executable, err)
			}
			killed[pid] = executable
		}
	}

	err = waitForProcessesToExit(killed)
	if err != nil {
		return err
	}

	// Stop BF2Hub from re-patching the binary
	err = r.OpenKey(registry.CURRENT_USER, "SOFTWARE\\BF2Hub Systems\\BF2Hub Client", registry.QUERY_VALUE|registry.SET_VALUE, func(key registry.Key) error {
		if err2 := key.SetDWordValue("hrpApplyOnStartup", 0); err2 != nil {
			return err2
		}

		if err2 := key.SetDWordValue("hrpInterval", 0); err2 != nil {
			return err2
		}

		return nil
	})
	if err != nil {
		// Ignore error if key does not exist, as it would indicate that the BF2Hub Client is not installed and thus
		// cannot interfere with patching
		if !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}

	return nil
}

func patchBinary(f finder, new provider) error {
	// Copied from https://github.com/cetteup/joinme.click-launcher/blob/089fb595adc426aab775fe40165431501a5c38c3/internal/titles/bf2.go#L37
	dir, err := f.GetInstallDirFromSomewhere([]software_finder.Config{
		{
			ForType:           software_finder.RegistryFinder,
			RegistryKey:       software_finder.RegistryKeyLocalMachine,
			RegistryPath:      "SOFTWARE\\WOW6432Node\\Electronic Arts\\EA Games\\Battlefield 2",
			RegistryValueName: "InstallDir",
		},
		{
			ForType:           software_finder.RegistryFinder,
			RegistryKey:       software_finder.RegistryKeyCurrentUser,
			RegistryPath:      "SOFTWARE\\BF2Hub Systems\\BF2Hub Client",
			RegistryValueName: "bf2Dir",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to determine Battlefield 2 install directory: %w", err)
	}

	path := filepath.Join(dir, bf2ExecutableName)

	stats, err := os.Stat(path)
	if err != nil {
		return err
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Detect "old"/current provider based on what's in the binary
	old, err := determineCurrentlyUsedProvider(original)
	if err != nil {
		return err
	}

	// No need to patch if binary is already patched as desired
	if new.Name == old.Name {
		return nil
	}

	modifications := getModifications(old, new)
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

	return os.WriteFile(path, modified, stats.Mode())
}

func determineCurrentlyUsedProvider(b []byte) (provider, error) {
	for _, p := range []provider{bf2hub, playbf2, openspy, gamespy} {
		ridges := append(p.Fingerprint.Additional, p.Fingerprint.Hostname, p.Fingerprint.HostsPath)
		if containsAll(b, ridges) {
			return p, nil
		}
	}

	return provider{}, fmt.Errorf("binary contains unknown/mixed modifications, revert changes first")
}

type modification struct {
	Old    []byte
	New    []byte
	Length int
	Count  int
}

func getModifications(old, new provider) []modification {
	// Default modifications, required for patching any provider
	modifications := []modification{
		{
			Old:    old.Fingerprint.HostsPath,
			New:    new.Fingerprint.HostsPath,
			Length: 18,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gamestats.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("gamestats.%s", new.Fingerprint.Hostname)),
			Length: 21,
			Count:  2,
		},
		{
			Old:    []byte(fmt.Sprintf("http://stage-net.%s/bf2/getplayerinfo.aspx?pid=", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("http://stage-net.%s/bf2/getplayerinfo.aspx?pid=", new.Fingerprint.Hostname)),
			Length: 56,
			Count:  1,
		},
		{
			Old: []byte(fmt.Sprintf("BF2Web.%s", old.Fingerprint.Hostname)),
			New: []byte(fmt.Sprintf("BF2Web.%s", new.Fingerprint.Hostname)),
			// Actual length of original is 18. However, "BF2Web.%s" would also match the below modification
			// and break the url, so add another trailing nil-byte to avoid the partial match
			Length: 19,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("http://BF2Web.%s/ASP/", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("http://BF2Web.%s/ASP/", new.Fingerprint.Hostname)),
			Length: 30,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("%%s.available.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.available.%s", new.Fingerprint.Hostname)),
			Length: 24,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("%%s.master.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.master.%s", new.Fingerprint.Hostname)),
			Length: 21,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gpcm.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("gpcm.%s", new.Fingerprint.Hostname)),
			Length: 16,
			Count:  1,
		},
		{
			Old:    []byte(fmt.Sprintf("gpsp.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("gpsp.%s", new.Fingerprint.Hostname)),
			Length: 16,
			Count:  1,
		},
	}

	// Semi backend-specific modifications (common for some backends)
	// Special case for PlayBF2: They remove the numeric placeholder/verb ("%d") in addition to changing the hostname
	if old.Name == playbf2.Name {
		// Remove "%d" when currently patched for PlayBF2
		modifications = append(modifications, modification{
			Old:    []byte(fmt.Sprintf("%%s.ms.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms%%d.%s", new.Fingerprint.Hostname)),
			Length: 19,
			Count:  1,
		})
	} else if new.Name == playbf2.Name {
		// Add "%d" when patching to PlayBF2
		modifications = append(modifications, modification{
			Old:    []byte(fmt.Sprintf("%%s.ms%%d.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms.%s", new.Fingerprint.Hostname)),
			Length: 19,
			Count:  1,
		})
	} else {
		// Symmetrical change for all other providers
		modifications = append(modifications, modification{
			Old:    []byte(fmt.Sprintf("%%s.ms%%d.%s", old.Fingerprint.Hostname)),
			New:    []byte(fmt.Sprintf("%%s.ms%%d.%s", new.Fingerprint.Hostname)),
			Length: 19,
			Count:  1,
		})
	}

	// Truly backend-specific modifications (unique to a single backend to be applied/reverted)
	switch old.Name {
	case bf2hub.Name:
		modifications = append(modifications, modification{
			Old:    []byte("bf2hbc.dll"),
			New:    []byte("WS2_32.dll"),
			Length: 10,
			Count:  1,
		},
		)
	}

	switch new.Name {
	case bf2hub.Name:
		modifications = append(modifications, modification{
			Old:    []byte("WS2_32.dll"),
			New:    []byte("bf2hbc.dll"),
			Length: 10,
			Count:  1,
		},
		)
	}

	return modifications
}
