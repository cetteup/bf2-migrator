//go:generate goversioninfo

package main

import (
	"os"

	filerepo "github.com/cetteup/filerepo/pkg"
	"github.com/cetteup/joinme.click-launcher/pkg/registry_repository"
	"github.com/cetteup/joinme.click-launcher/pkg/software_finder"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/cetteup/conman/pkg/handler"

	"github.com/cetteup/bf2-migrator/cmd/bf2-migrator/internal/gui"
	"github.com/cetteup/bf2-migrator/pkg/gamespy"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
}

func main() {
	fileRepository := filerepo.New()
	registryRepository := registry_repository.New()
	h := handler.New(fileRepository)

	f := software_finder.New(registryRepository, fileRepository)
	c := gamespy.NewClient(10)
	mw, err := gui.CreateMainWindow(h, f, registryRepository, c)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create main window")
	}

	mw.Run()
}
