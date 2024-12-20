# Obsidian Live Syncinator Server

The Obsidian-Live-Syncinator-Server is the server of the plugin [syncinator](https://github.com/hiimjako/obsidian-live-syncinator).
# Setup

Create a `.env`:
```sh
JWT_SECRET=secret
STORAGE_DIR=/data
SQLITE_FILEPATH=/data/db.sqlite3
```

Start the docker container: 
```sh
docker run --name obsidian-live-syncinator-server ghcr.io/hiimjako/obsidian-live-syncinator-server -p 8080:8080 --env-file .env
```

## Create a new Workspace
```sh
docker exec obsidian-live-syncinator-server ./cli -name "workspace-name" -pass "strong-pass" -db "./data/db.sqlite3"
```

> [!WARNING]  
> The `db` argument must be the same as `SQLITE_FILEPATH` env variable.


Docker compose example:
```sh 
services:
  syncinator:
    container_name: obsidian-live-syncinator-server 
    image: ghcr.io/hiimjako/obsidian-live-syncinator-server:main
    env_file: .env
    restart: always
    volumes:
        - data:/usr/src/data
    ports:
      - 8080:8080

volumes:
    data:
```


# Development
## Add new migration

```sh 
GOOSE_DRIVER=sqlite GOOSE_MIGRATION_DIR=./internal/migration/migrations/ goose create new_migration_name sql
```
