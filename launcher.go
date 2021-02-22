package main

import (
	"BSWLauncher/util"
	"encoding/json"
	"fmt"
	"github.com/howeyc/gopass"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

const ConfigFile string = "launcher_config.json"

type LauncherInfo struct {
	GameServer string `json:"game-server"`
	GamePort   int    `json:"game-port"`
	Version    string `json:"version"`
}

func getLoginInfo() *Config {
	config, err := loadConfig()
	if err == nil {
		if config.Password, err = util.Decrypt(config.Password); err != nil {
			config = nil
		}
	}

	var username string
	var password string

	if err != nil {
		args := os.Args[1:]
		if len(args) > 0 {
			username, args = args[0], args[1:]
		} else {
			for username == "" {
				fmt.Print("Enter your username: ")
				_, _ = fmt.Scanln(&username)
			}
		}

		if len(args) > 0 {
			password, args = args[0], args[1:]
		} else {
			for password == "" {
				fmt.Print("Enter your password: ")
				passwordBytes, err := gopass.GetPasswdMasked()
				if err == nil {
					password = string(passwordBytes)
				}
			}
		}

	}

	if config == nil {
		config = &Config{
			Username: username,
			Password: password,
		}
		fmt.Print("Would you like to save this information for next time [y / n]? ")
		if askForConfirmation() {
			config.Save()
		}
	}

	if config.Username == "" || config.Password == "" {
		log.Panic("Please enter a username and a password.")
	}

	return config
}

func fetchLoginToken(client *http.Client, config *Config) string {
	form := url.Values{}
	form.Add("username", config.Username)
	form.Add("password", config.Password)

	req, err := http.NewRequest("POST", "https://burningsw.to/login", strings.NewReader(form.Encode()))
	if err != nil {
		log.Panic("Error creating request", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	resp, err := client.Do(req)
	if err != nil {
		log.Panic("Could not send login request", err)
	}
	resp.Body.Close()

	req, err = http.NewRequest("POST", "https://burningsw.to/api/generate_token", nil)
	if err != nil {
		log.Panic("Error posting to login api", err)
	}

	resp, err = client.Do(req)
	if err != nil {
		log.Panic("Error getting login token", err)
	}
	defer resp.Body.Close()

	res, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Panic("Could not read token response", err)
	}
	tokenA := strings.Split(string(res), "&")
	token := tokenA[0]
	if len(tokenA) != 2 || token == "" {
		config.Username = ""
		config.Password = ""
		config.Save()
		log.Panic("Invalid username or password, or your account has not been activated (check your email).")
	}

	return token
}

func fetchLauncherInfo(client *http.Client) *LauncherInfo {
	resp, err := client.Get("https://launcher.burningsw.to/info.json")
	if err != nil {
		log.Panic("Could not get launcher info", err)
	}
	defer resp.Body.Close()
	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		log.Panic("Could not read launcher info", err)
	}

	launcherInfo := &LauncherInfo{}
	err = json.Unmarshal(body, launcherInfo)
	if err != nil {
		log.Panic("Could not unmarshall launcher info", err)
	}

	return launcherInfo
}

func launch(username string, token string, server string, port int) {
	cmd := exec.Command("BurningSW.exe", "HID:"+username, "TOKEN:"+token, "CHCODE:11", "IP:"+server, fmt.Sprintf("PORT:%v", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Println("Error: ", err)
	}
	_ = cmd.Wait()
}

func containsString(slice []string, element string) bool {
	for _, elem := range slice {
		if elem == element {
			return true
		}
	}
	return false
}

func askForConfirmation() bool {
	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		log.Panic(err)
	}
	yesResponses := []string{"y", "Y", "yes", "Yes", "YES"}
	noResponses := []string{"n", "N", "no", "No", "NO"}
	if containsString(yesResponses, response) {
		return true
	} else if containsString(noResponses, response) {
		return false
	} else {
		log.Print("Please type yes or no and then press enter: ")
		return askForConfirmation()
	}
}
