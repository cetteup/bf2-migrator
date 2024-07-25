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
	api "github.com/cetteup/bf2-migrator/pkg/openspy"
	"github.com/cetteup/bf2-migrator/pkg/patch"
)

const (
	windowWidth  = 290
	windowHeight = 350

	bf2hubExecutableName = "bf2hub.exe"
)

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

type providerCBOption struct {
	Name     string
	Provider patch.Provider
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
	var pathTE *walk.TextEdit
	var providerCB *walk.ComboBox
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
				Title:  "Migrate",
				Name:   "Migrate",
				Layout: declarative.VBox{},
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
								AssignTo:      &providerCB,
								DisplayMember: "Name",
								BindingMember: "Provider",
								Name:          "Select provider",
								ToolTipText:   "Select provider",
								Model: []providerCBOption{
									// Not offering BF2Hub (needs a .dll in addition to .exe changes)
									{
										Name:     string(patchable.ProviderPlayBF2),
										Provider: patchable.ProviderPlayBF2,
									},
									{
										Name:     string(patchable.ProviderOpenSpy),
										Provider: patchable.ProviderOpenSpy,
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

											selected := providerCB.Model().([]providerCBOption)[providerCB.CurrentIndex()]
											err2 = patchAll(patchables, pathTE.Text(), selected.Provider)
											if err2 != nil {
												walk.MsgBox(mw, "Error", fmt.Sprintf("Failed to patch %s", err2.Error()), walk.MsgBoxIconError)
											} else {
												walk.MsgBox(mw, "Success", fmt.Sprintf("Patched game to use %s", selected.Name), walk.MsgBoxIconInformation)
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
				Text:       "BF2 migrator v0.5.0",
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
			return fmt.Errorf("%s: %w", p.GetFileName(), err)
		}
	}

	return nil
}
