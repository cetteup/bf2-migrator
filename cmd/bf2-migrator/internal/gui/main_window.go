package gui

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/cetteup/conman/pkg/game/bf2"
	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"github.com/cetteup/conman/pkg/game"
	"github.com/cetteup/joinme.click-launcher/pkg/software_finder"

	"github.com/dogclan/bf2-migrator/pkg/openspy"
)

const (
	windowWidth  = 300
	windowHeight = 290

	gamespyHostname   = "gamespy.com"
	openspyHostname   = "openspy.net"
	playbf2Hostname   = "playbf2.ru"
	bf2hubPatcherName = "BF2Hub Patcher"
	bf2hubDLLName     = "bf2hbc.dll"
	occurrences       = 10
)

type client interface {
	CreateAccount(email, password string, partnerCode int) error
	CreateProfile(nick string, namespaceID int) error
	GetProfiles() ([]openspy.ProfileDTO, error)
}

type finder interface {
	GetInstallDirFromSomewhere(configs []software_finder.Config) (string, error)
}

type DropDownItem struct { // Used in the ComboBox dropdown
	Key  int
	Name string
}

func CreateMainWindow(h game.Handler, c client, f finder, profiles []game.Profile, defaultProfileKey string) (*walk.MainWindow, error) {
	icon, err := walk.NewIconFromResourceIdWithSize(2, walk.Size{Width: 256, Height: 256})
	if err != nil {
		return nil, err
	}

	screenWidth := win.GetSystemMetrics(win.SM_CXSCREEN)
	screenHeight := win.GetSystemMetrics(win.SM_CYSCREEN)

	profileOptions, selectedProfile, err := computeProfileSelectOptions(profiles, defaultProfileKey)
	if err != nil {
		return nil, err
	}

	var mw *walk.MainWindow
	var selectCB *walk.ComboBox
	var migratePB *walk.PushButton
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
				AssignTo:      &selectCB,
				Value:         profileOptions[selectedProfile].Key,
				Model:         profileOptions,
				DisplayMember: "Name",
				BindingMember: "Key",
				Name:          "Select profile",
				ToolTipText:   "Select profile",
				OnCurrentIndexChanged: func() {
					// Password actions cannot be used with singleplayer profiles, since those don't have passwords
					if profiles[selectCB.CurrentIndex()].Type == game.ProfileTypeMultiplayer {
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

							profile := profiles[selectCB.CurrentIndex()]
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
			declarative.GroupBox{
				Title:  "Global actions",
				Name:   "Global actions",
				Layout: declarative.VBox{},
				Children: []declarative.Widget{
					declarative.PushButton{
						AssignTo: &patchPB,
						Text:     "Patch BF2.exe to use OpenSpy",
						OnClicked: func() {
							// Block any actions during patching
							mw.SetEnabled(false)
							_ = patchPB.SetText("Patching...")
							defer func() {
								_ = patchPB.SetText("Patch BF2.exe to use OpenSpy")
								mw.SetEnabled(true)
							}()

							err2 := patchBinary(f, gamespyHostname, openspyHostname)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch BF2.exe: %s", err2.Error()), walk.MsgBoxIconError)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Patched BF2.exe to use OpenSpy\n\nRevert patch before using %q to use BF2Hub again", bf2hubPatcherName), walk.MsgBoxIconInformation)
							}
						},
					},
					declarative.PushButton{
						AssignTo: &revertPB,
						Text:     "Revert BF2.exe to use GameSpy",
						OnClicked: func() {
							// Block any actions during patching
							mw.SetEnabled(false)
							_ = revertPB.SetText("Reverting...")
							defer func() {
								_ = revertPB.SetText("Revert BF2.exe to use GameSpy")
								mw.SetEnabled(true)
							}()

							err2 := patchBinary(f, openspyHostname, gamespyHostname)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch BF2.exe: %s", err2.Error()), walk.MsgBoxIconError)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Reverted BF2.exe to to use GameSpy\n\nUse %q to use BF2Hub again", bf2hubPatcherName), walk.MsgBoxIconInformation)
							}
						},
					},
				},
			},
			declarative.Label{
				Text:       "BF2 migrator v0.2.1",
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

	return mw, nil
}

func computeProfileSelectOptions(profiles []game.Profile, defaultProfileKey string) ([]DropDownItem, int, error) {
	defaultOption := 0
	options := make([]DropDownItem, 0, len(profiles))
	for i, profile := range profiles {
		key, err := strconv.Atoi(profile.Key)
		if err != nil {
			return nil, 0, err
		}

		if profile.Key == defaultProfileKey {
			defaultOption = i
		}

		options = append(options, DropDownItem{
			Key:  key,
			Name: profile.Name,
		})
	}

	return options, defaultOption, nil
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

	exists := slices.ContainsFunc(profiles, func(profile openspy.ProfileDTO) bool {
		return profile.UniqueNick == nick && profile.NamespaceID == 12
	})

	if !exists {
		err2 := c.CreateProfile(nick, 12)
		if err2 != nil {
			return fmt.Errorf("failed to create OpenSpy profile: %w", err2)
		}
	}

	return nil
}

func patchBinary(f finder, old, new string) error {
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

	path := filepath.Join(dir, "BF2.exe")

	stats, err := os.Stat(path)
	if err != nil {
		return err
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if bytes.Contains(original, []byte(bf2hubDLLName)) {
		return fmt.Errorf("binary is currently patched for BF2Hub, use %q to revert patches first", bf2hubPatcherName)
	}
	if bytes.Contains(original, []byte(playbf2Hostname)) {
		return fmt.Errorf("binary is currently patched for PlayBF2, revert patches first")
	}
	// If binary contains neither old nor new the expected number of times, something's off with the binary
	// (comparing both to avoid returning an error if a binary is already in the target state of patching)
	if bytes.Count(original, []byte(old)) != occurrences && bytes.Count(original, []byte(new)) != occurrences {
		return fmt.Errorf("binary contains unknown modifications, revert changes first")
	}

	modified := bytes.ReplaceAll(original, []byte(old), []byte(new))

	// No need to write if binary is already patched as desired
	if bytes.Equal(modified, original) {
		return nil
	}

	return os.WriteFile(path, modified, stats.Mode())
}
