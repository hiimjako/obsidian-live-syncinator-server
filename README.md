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

> [!NOTE]  
> The container uses WebSockets, so be sure to enable it if you run the service under reverse proxy.


## Create a new Workspace
```sh
docker exec obsidian-live-syncinator-server ./cli -name "workspace-name" -pass "strong-pass" -db "./data/db.sqlite3"
```

> [!IMPORTANT]  
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

# TODO

- Lazy load files in memory and remove from memory after some time, to reduce the memory usage on unmodified files.
- Create a GET /export to export workspace as zip
- Create cluster of servers
- Ad DST (deterministic simulation testing) to test chunks
