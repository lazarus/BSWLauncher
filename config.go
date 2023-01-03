package main

import (
	"BSWLauncher/util"
	"encoding/json"
	"errors"
	"os"
)

type Config struct {
	Username string
	Password string
}

func loadConfig() (*Config, error) {
	config := &Config{}
	file, err := os.Open(ConfigFile)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if config.Username == "" || config.Password == "" {
		return nil, errors.New("invalid username or password")
	}
	return config, err
}

func (conf Config) Save() {
	pw, err := util.Encrypt(conf.Password)
	if err != nil {
		panic(err)
	}
	conf.Password = pw
	file, _ := json.Marshal(conf)
	_ = os.WriteFile(ConfigFile, file, 0644)
}
