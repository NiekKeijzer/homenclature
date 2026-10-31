// Command homenclature serves a household homepage: links per OIDC group.
package main

import (
	"log"

	"github.com/NiekKeijzer/homenclature/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
