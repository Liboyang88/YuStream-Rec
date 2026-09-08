package bootstrap

import (
	"log"
	"github.com/spf13/viper"
	"dcpro/domain"
)

func NewEnv() *domain.Env {
	env := domain.Env{}
	viper.SetConfigFile(".env")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal("Can't find the file .env : ", err)
	}

	err = viper.Unmarshal(&env)
	if err != nil {
		log.Fatal("Environment can't be loaded: ", err)
	}

	return &env
}