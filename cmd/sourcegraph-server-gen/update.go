package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"

	update "github.com/inconshreveable/go-update"
)

func doUpdate(version string) error {
	var url string
	switch runtime.GOOS {
	case "linux":
		url = "https://storage.googleapis.com/sourcegraph-assets/sourcegraph-server-gen/linux_amd64/sourcegraph-server-gen"
		if version != "" {
			url = fmt.Sprintf("https://storage.googleapis.com/sourcegraph-assets/sourcegraph-server-gen/%s/linux_amd64/sourcegraph-server-gen", version)
		}
	case "darwin":
		url = "https://storage.googleapis.com/sourcegraph-assets/sourcegraph-server-gen/darwin_amd64/sourcegraph-server-gen"
		if version != "" {
			url = fmt.Sprintf("https://storage.googleapis.com/sourcegraph-assets/sourcegraph-server-gen/%s/darwin_amd64/sourcegraph-server-gen", version)
		}
	default:
		fmt.Println("Unsupported operating system (must run on macOS or Linux)")
		os.Exit(1)
	}

	// request the new file
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Version %s was not found. Status code was %v.", version, resp.StatusCode)
	}
	err = update.Apply(resp.Body, update.Options{})
	if err != nil {
		if rerr := update.RollbackError(err); rerr != nil {
			fmt.Println("Failed to rollback from bad update:", rerr)
		}
	}
	return err
}
