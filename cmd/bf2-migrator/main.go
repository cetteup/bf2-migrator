//go:generate goversioninfo

package main

import (
	"os"

	filerepo "github.com/cetteup/filerepo/pkg"
	"github.com/cetteup/joinme.click-launcher/pkg/registry_repository"
	"github.com/cetteup/joinme.click-launcher/pkg/software_finder"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/cetteup/conman/pkg/game/bf2"
	"github.com/cetteup/conman/pkg/handler"

	"github.com/dogclan/bf2-migrator/cmd/bf2-migrator/internal/gui"
	"github.com/dogclan/bf2-migrator/pkg/openspy"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
}

func main() {
	fileRepository := filerepo.New()
	registryRepository := registry_repository.New()
	h := handler.New(fileRepository)

	profiles, err := bf2.GetProfiles(h)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get list of available profiles")
	}

	defaultProfileKey, err := bf2.GetDefaultProfileKey(h)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to determine current default user profile key")
	}

	c := openspy.New(openspy.BaseURL, 10)
	f := software_finder.New(registryRepository, fileRepository)
	mw, err := gui.CreateMainWindow(h, c, f, registryRepository, profiles, defaultProfileKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create main window")
	}

	mw.Run()
}
