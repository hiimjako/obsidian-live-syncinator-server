package env

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

type EnvVariables struct {
	Host string `env:"HOST,default=127.0.0.1"`
	Port string `env:"PORT,default=8080"`

	StorageDir     string `env:"STORAGE_DIR,default=./data"`
	SqliteFilepath string `env:"SQLITE_FILEPATH,default=./data/db.sqlite3"`
}

func LoadEnv(paths ...string) *EnvVariables {
	path := ".env"
	if len(paths) > 0 {
		path = paths[0]
	}

	if err := godotenv.Load(path); err != nil {
		log.Println("error loading env file:", err)
	}

	var env EnvVariables
	ctx := context.Background()
	if err := envconfig.Process(ctx, &env); err != nil {
		panic(err)
	}

	err := os.MkdirAll(env.StorageDir, 0755)
	if err != nil {
		panic(err)
	}

	return &env
}
