package env

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

type EnvVariables struct {
	Host string `env:"HOST,default=0.0.0.0"`
	Port string `env:"PORT,default=8080"`

	StorageDir          string        `env:"STORAGE_DIR,default=./data"`
	SqliteFilepath      string        `env:"SQLITE_FILEPATH,default=./data/db.sqlite3"`
	JWTSecret           []byte        `env:"JWT_SECRET,required"`
	CacheMaxAge         time.Duration `env:"CACHE_MAX_AGE,default=1h"`
	FlushInterval       time.Duration `env:"FLUSH_INTERVAL,default=1m"`
	MaxFileSizeMB       int64         `env:"MAX_FILE_SIZE,default=1024"`
	MinChangesThreshold int64         `env:"MIN_CHANGES_THRESHOLD,default=3"`
}

func LoadEnv(paths ...string) *EnvVariables {
	path := ".env"
	if len(paths) > 0 {
		path = paths[0]
	}

	if err := godotenv.Load(path); err == nil {
		log.Println("found .env file, overriding envs")
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
