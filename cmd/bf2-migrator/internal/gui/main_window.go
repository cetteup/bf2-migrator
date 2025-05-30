package gui

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/cetteup/conman/pkg/game/bf2"
	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/mitchellh/go-ps"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"

	"github.com/cetteup/conman/pkg/game"
	"github.com/cetteup/joinme.click-launcher/pkg/software_finder"

	"github.com/cetteup/bf2-migrator/cmd/bf2-migrator/internal/patchable"
	"github.com/cetteup/bf2-migrator/pkg/gamespy"
	"github.com/cetteup/bf2-migrator/pkg/patch"
)

const (
	windowWidth  = 290
	windowHeight = 412

	bf2hubExecutableName = "bf2hub.exe"

	providerNameBF2Hub  = "BF2Hub"
	providerNamePlayBF2 = "PlayBF2"
	providerNameOpenSpy = "OpenSpy"
)

type finder interface {
	GetInstallDirFromSomewhere(configs []software_finder.Config) (string, error)
}

type registryRepository interface {
	OpenKey(k registry.Key, path string, access uint32, cb func(key registry.Key) error) error
}

type client interface {
	GetNicks(provider gamespy.Provider, email, password string) ([]gamespy.NickDTO, error)
	CreateUser(provider gamespy.Provider, email, password, nick string) error
}

type providerCBOption[T patch.Provider | gamespy.Provider] struct {
	Name  string
	Value T
}

func CreateMainWindow(h game.Handler, f finder, r registryRepository, c client) (*walk.MainWindow, error) {
	icon, err := walk.NewIconFromResourceIdWithSize(2, walk.Size{Width: 256, Height: 256})
	if err != nil {
		return nil, err
	}

	screenWidth := win.GetSystemMetrics(win.SM_CXSCREEN)
	screenHeight := win.GetSystemMetrics(win.SM_CYSCREEN)

	var mw *walk.MainWindow
	var migrateGB *walk.GroupBox
	var profileCB *walk.ComboBox
	var migrateProviderCB *walk.ComboBox
	var migratePB *walk.PushButton
	var pathTE *walk.TextEdit
	var patchProviderCB *walk.ComboBox
	var patchPB *walk.PushButton
	var revertPB *walk.PushButton

	enablePatch := func(path string) {
		_ = pathTE.SetText(path)
		_ = pathTE.SetToolTipText(path)
		patchPB.SetEnabled(true)
		revertPB.SetEnabled(true)
	}

	patchables := []patch.Patchable{
		patchable.GameExecutable{},
		patchable.ServerExecutable{},
	}

	if err = (declarative.MainWindow{
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
			declarative.GroupBox{
				AssignTo: &migrateGB,
				Title:    "Migrate",
				Name:     "Migrate",
				Layout:   declarative.VBox{},
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
					declarative.Label{
						Text:       "Select provider",
						TextColor:  walk.Color(win.GetSysColor(win.COLOR_CAPTIONTEXT)),
						Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
					},
					declarative.ComboBox{
						AssignTo:      &migrateProviderCB,
						DisplayMember: "Name",
						BindingMember: "Value",
						Name:          "Select provider",
						ToolTipText:   "Select provider",
						Model: []providerCBOption[gamespy.Provider]{
							{
								Name:  providerNameBF2Hub,
								Value: gamespy.ProviderBF2Hub,
							},
							{
								Name:  providerNamePlayBF2,
								Value: gamespy.ProviderPlayBF2,
							},
							{
								Name:  providerNameOpenSpy,
								Value: gamespy.ProviderOpenSpy,
							},
							// Not offering GameSpy (obsolete, cannot migrate anything to it)
						},
						CurrentIndex: 2, // Select OpenSpy as default
					},
					declarative.PushButton{
						AssignTo: &migratePB,
						Text:     "Migrate profile",
						OnClicked: func() {
							// Block any actions during migrations
							mw.SetEnabled(false)
							_ = migratePB.SetText("Migrating...")
							defer func() {
								_ = migratePB.SetText("Migrate profile")
								mw.SetEnabled(true)
							}()

							provider := migrateProviderCB.Model().([]providerCBOption[gamespy.Provider])[migrateProviderCB.CurrentIndex()]
							profile := profileCB.Model().([]game.Profile)[profileCB.CurrentIndex()]
							migrated, err2 := migrateProfile(h, c, provider.Value, profile.Key)
							if err2 != nil {
								walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to migrate %q to %s: %s", profile.Name, provider.Name, err2.Error()), walk.MsgBoxIconError)
							} else if !migrated {
								walk.MsgBox(mw, "Skipped", fmt.Sprintf("%q is already set up on %s", profile.Name, provider.Name), walk.MsgBoxIconInformation)
							} else {
								walk.MsgBox(mw, "Success", fmt.Sprintf("Migrated %q to %s", profile.Name, provider.Name), walk.MsgBoxIconInformation)
							}
						},
					},
				},
			},
			declarative.GroupBox{
				Title:  "Patch",
				Name:   "Patch",
				Layout: declarative.VBox{},
				Children: []declarative.Widget{
					declarative.Label{
						Text:       "Installation folder",
						TextColor:  walk.Color(win.GetSysColor(win.COLOR_CAPTIONTEXT)),
						Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
					},
					declarative.TextEdit{
						AssignTo: &pathTE,
						Name:     "Installation folder",
						ReadOnly: true,
					},
					declarative.HSplitter{
						Children: []declarative.Widget{
							declarative.PushButton{
								Text: "Detect",
								OnClicked: func() {
									detected, err2 := detectInstallPath(f)
									if err2 != nil {
										walk.MsgBox(mw, "Warning", "Could not detect game installation folder, please choose the path manually", walk.MsgBoxIconWarning)
										return
									}

									enablePatch(detected)
								},
							},
							declarative.PushButton{
								Text: "Choose",
								OnClicked: func() {
									dlg := &walk.FileDialog{
										Title: "Choose installation folder",
									}

									ok, err2 := dlg.ShowBrowseFolder(mw)
									if err2 != nil {
										walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to choose installation folder: %s", err2.Error()), walk.MsgBoxIconError)
										return
									} else if !ok {
										// User canceled dialog
										return
									}

									enablePatch(dlg.FilePath)
								},
							},
						},
					},
					declarative.VSpacer{Size: 1},
					declarative.Composite{
						Layout: declarative.VBox{
							MarginsZero: true,
						},
						Children: []declarative.Widget{
							declarative.Label{
								Text:       "Select provider",
								TextColor:  walk.Color(win.GetSysColor(win.COLOR_CAPTIONTEXT)),
								Background: declarative.SolidColorBrush{Color: walk.Color(win.GetSysColor(win.COLOR_BTNFACE))},
							},
							declarative.ComboBox{
								AssignTo:      &patchProviderCB,
								DisplayMember: "Name",
								BindingMember: "Value",
								Name:          "Select provider",
								ToolTipText:   "Select provider",
								Model: []providerCBOption[patch.Provider]{
									// Not offering BF2Hub (needs a .dll in addition to .exe changes)
									{
										Name:  providerNamePlayBF2,
										Value: patchable.ProviderPlayBF2,
									},
									{
										Name:  providerNameOpenSpy,
										Value: patchable.ProviderOpenSpy,
									},
									// Not offering GameSpy (obsolete, only used for reverting)
								},
								CurrentIndex: 1, // Select OpenSpy as default
							},
							declarative.HSplitter{
								Children: []declarative.Widget{
									declarative.PushButton{
										AssignTo: &patchPB,
										Text:     "Apply patch",
										Enabled:  false,
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
												walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to prepare for patching: %s", err2.Error()), walk.MsgBoxIconError)
												return
											}

											provider := patchProviderCB.Model().([]providerCBOption[patch.Provider])[patchProviderCB.CurrentIndex()]
											err2 = patchAll(patchables, pathTE.Text(), provider.Value)
											if err2 != nil {
												walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch %s", err2.Error()), walk.MsgBoxIconError)
											} else {
												walk.MsgBox(mw, "Success", fmt.Sprintf("Patched game to use %s", provider.Name), walk.MsgBoxIconInformation)
											}
										},
									},
									declarative.PushButton{
										AssignTo: &revertPB,
										Text:     "Revert patch",
										Enabled:  false,
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
												walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to prepare for reverting: %s", err2.Error()), walk.MsgBoxIconError)
												return
											}

											err2 = patchAll(patchables, pathTE.Text(), patchable.ProviderGameSpy)
											if err2 != nil {
												walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch %s", err2.Error()), walk.MsgBoxIconError)
											} else {
												walk.MsgBox(mw, "Success", "Reverted game to use GameSpy\n\nYou can now use provider-specific patchers again (e.g. BF2Hub Patcher)", walk.MsgBoxIconInformation)
											}
										},
									},
								},
							},
						},
					},
				},
			},
			declarative.Label{
				Text:       "BF2 migrator v0.7.1",
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
		walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to load profiles: %s\n\nProfile migration will not be available", err.Error()), walk.MsgBoxIconError)
		_ = migrateGB.SetTitle("Migrate (unavailable: failed to load profiles)")
		migrateProviderCB.SetEnabled(false)
		profileCB.SetEnabled(false)
		migratePB.SetEnabled(false)
	} else if len(profiles) == 0 {
		_ = migrateGB.SetTitle("Migrate (unavailable: no profiles found)")
		migrateProviderCB.SetEnabled(false)
		profileCB.SetEnabled(false)
		migratePB.SetEnabled(false)
	} else {
		_ = profileCB.SetModel(profiles)
		_ = profileCB.SetCurrentIndex(selected)
	}

	// Automatically try to detect install path once, pre-filling path if path is detected
	detected, err := detectInstallPath(f)
	if err == nil {
		enablePatch(detected)
	}

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

func migrateProfile(h game.Handler, c client, provider gamespy.Provider, profileKey string) (bool, error) {
	profileCon, err := bf2.ReadProfileConfigFile(h, profileKey, bf2.ProfileConfigFileProfileCon)
	if err != nil {
		return false, fmt.Errorf("failed to read profile config file: %w", err)
	}

	nick, encrypted, err := bf2.GetEncryptedLogin(profileCon)
	if err != nil {
		return false, fmt.Errorf("failed to get encrypted login from profile config file: %w", err)
	}

	password, err := bf2.DecryptProfileConPassword(encrypted)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt profile password: %w", err)
	}

	email, err := profileCon.GetValue(bf2.ProfileConKeyEmail)
	if err != nil {
		return false, fmt.Errorf("failed to get email address from profile config file: %w", err)
	}

	nicks, err := c.GetNicks(provider, email.String(), password)
	if err != nil {
		return false, fmt.Errorf("failed to get OpenSpy account profiles: %w", err)
	}

	// Don't use slices package here to maintain compatibility with go 1.20 (and thus Windows 7)
	for _, profile := range nicks {
		if profile.UniqueNick == nick {
			return false, nil
		}
	}

	err2 := c.CreateUser(provider, email.String(), password, nick)
	if err2 != nil {
		return false, fmt.Errorf("failed to create OpenSpy profile: %w", err2)
	}

	return true, nil
}

func prepareForPatch(r registryRepository) error {
	processes, err := ps.Processes()
	if err != nil {
		return fmt.Errorf("failed to retrieve process list: %s", err)
	}

	killed := map[int]string{}
	for _, process := range processes {
		executable := process.Executable()
		if executable == patchable.GameExecutableName || executable == patchable.ServerExecutableName || executable == bf2hubExecutableName {
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

func detectInstallPath(f finder) (string, error) {
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
		return "", fmt.Errorf("failed to determine Battlefield 2 install directory: %w", err)
	}

	return dir, err
}

func patchAll(patchables []patch.Patchable, dir string, new patch.Provider) error {
	for _, p := range patchables {
		if err := patch.Patch(p, dir, new); err != nil {
			// Server executable is optional and not included with some installers for the game
			if errors.Is(err, patch.ErrNotExist) && p.GetFileName() == patchable.ServerExecutableName {
				return nil
			}
			return fmt.Errorf("%s: %w", p.GetFileName(), err)
		}
	}

	return nil
}
