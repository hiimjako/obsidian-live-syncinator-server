# Obsidian Live Syncinator Server

The Obsidian-Live-Syncinator-Server is the server of the plugin [syncinator](https://github.com/hiimjako/obsidian-live-syncinator).

# Setup

Create a `.env`:

```sh
JWT_SECRET=secret
STORAGE_DIR=./data
SQLITE_FILEPATH=./data/db.sqlite3
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

# Synchronization Logic

The synchronization of text files between clients is achieved using a central server and the Operational Transformation (OT) algorithm.
This ensures that all clients converge eventually to the same document state, even when multiple users are making changes concurrently.

## Architecture

The system follows a client-server architecture:

- **Server:** A central, authoritative server that manages the document state and orchestrates the synchronization process.
- **Clients:** Each client (e.g., the Obsidian plugin) maintains a local copy of the document and communicates with the server to send and receive changes.

## Synchronization Process

1. **Client Sends Changes:** When a user modifies a document, the client computes the changes as a set of operations (insertions and deletions) and sends them to the server along with the current version of the document it is based on.
2. **Server Receives and Transforms Changes:**
   - The server receives the operations from the client.
   - It checks the version number of the incoming operations. If the version is older than the server's current version of the document, it means other clients have made changes in the meantime.
   - The server then transforms the incoming operations against the operations that have been applied since the client's version. This is the core of the OT algorithm, ensuring that the client's changes are correctly applied to the current state of the document.
3. **Server Applies and Broadcasts Changes:**
   - After transformation, the server applies the new operations to its copy of the document and increments its version number.
   - The server then broadcasts the transformed operations to all other connected clients.
4. **Clients Receive and Apply Changes:**
   - Other clients receive the broadcasted operations from the server.
   - They apply these operations to their local copy of the document, ensuring that they stay in sync with the server.

This process guarantees that all clients will eventually have the same content, resolving conflicts in a consistent and predictable manner.

# Disclaimer

This is recreational software provided as-is, without any warranty. While the plugin is functional, I do not assume any responsibility for potential data loss or other issues that may arise from its use. Always maintain backups of your important data before using any synchronization tools.

# TODO

- Create cluster of servers
- Add DST (deterministic simulation testing) to test chunks
- Add optional encryption to files
- Add OpenTelemetry instrumentation (tracing OT operations, HTTP middleware, broadcast)
