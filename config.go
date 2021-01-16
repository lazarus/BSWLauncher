package main

import (
	"BSWLauncher/util"
	"encoding/json"
	"io/ioutil"
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
	return config, err
}

func (conf Config) Save() {
	pw, err := util.Encrypt(conf.Password)
	if err != nil {
		panic(err)
	}
	conf.Password = pw
	file, _ := json.Marshal(conf)
	_ = ioutil.WriteFile(ConfigFile, file, 0644)
}
