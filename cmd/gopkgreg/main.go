// Command gopkgreg signs in to a gopkgreg registry and publishes Go modules
// from git tags.
//
//	gopkgreg login --registry https://gopkgreg.com
//	git tag v1.0.0
//	gopkgreg publish
package main

import (
	"os"

	"github.com/gopkgreg/gopkgreg/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.OSEnv()))
}
