package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"runtime"
)

const (
	CDN   = false
	LOCAL = true
)

func main() {
	// Catch Error/Panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Print("Press 'Enter' to continue...")
			_, _ = fmt.Scanln()
			os.Exit(1)
		}
	}()

	// Patcher
	log.Println("Checking download server status")
	if onlineCDNs, onlineServers = checkCDNStatus(); onlineServers == 0 {
		log.Panic("There are no download servers online. Message the BSW admins if there is no post in #news already.")
	}

	log.Println("Fetching version file")
	cdnFile, err := fetchVersionFile(CDN)
	if err != nil {
		log.Panic("Could not fetch remote version file", err)
	}
	localVersionDB, err = fetchVersionFile(LOCAL)

	var toDownload []File
	if err != nil {
		localVersionDB = &VersionFile{}
		toDownload = verifyFiles(cdnFile.Files)
		if err = localVersionDB.save(); err != nil {
			log.Panic(err)
		}
	} else {
		log.Println("Diff version file")
		toDownload = diffVersionFile(cdnFile, localVersionDB)
	}
	log.Printf("Fetched version information for %v/%v files.\n", localVersionDB.NumberOfFiles, cdnFile.NumberOfFiles)

	if len(toDownload) == 0 {
		log.Println("There are no files that need updating.")
	}
	for len(toDownload) > 0 {
		log.Printf("Found %v files that need to be updated.\n", len(toDownload))

		downloadFiles(toDownload, runtime.NumCPU())

		toDownload = verifyFiles(cdnFile.Files) // Verify manually and when download is done
	}

	//cmd := exec.Command("cmd", "/c", "cls") //Windows example, its tested
	//cmd.Stdout = os.Stdout
	//_ = cmd.Run()

	// Launcher
	config := getLoginInfo()

	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Panic("Could not setup cookie jar", err)
	}
	client := http.Client{Jar: jar}

	log.Println("Logging in")
	token := fetchLoginToken(&client, config)
	launcherInfo := fetchLauncherInfo(&client)

	log.Println("Launching BSW...")
	launch(config.Username, token, launcherInfo.GameServer, launcherInfo.GamePort)
}
