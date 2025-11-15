# PocketBase HA
Highly Available Leader/Leaderless [PocketBase](https://pocketbase.io/) Cluster powered by `go-ha` [database/sql driver](https://github.com/litesql/go-ha).

## Features

- **High Availability**: Run multiple PocketBase instances in a leaderless cluster.
- **Replication**: Synchronize data across nodes using NATS.
- **Embedded or External NATS**: Choose between an embedded NATS server or an external one for replication.

## Prerequisites

- **Go**: Version `1.25` or later is required.

## Installation

Download from [releases page](https://github.com/litesql/pocketbase-ha/releases/latest/).

### Install from source

Install the latest version of `pocketbase-ha` using:

```sh
go install github.com/litesql/pocketbase-ha@latest
```

### Docker image

```sh
docker pull ghcr.io/litesql/pocketbase-ha:latest
```

### Install from helm

- Add [litesql helm charts repository](https://litesql.github.io/helm-charts) to Helm:

```sh
helm repo add litesql https://litesql.github.io/helm-charts
```

- Update the chart repository:

```sh
helm repo update
```

- Deploy ha to kubernetes:

```sh
helm install pb litesql/pocketbase-ha
```

- Visit [litesql helm charts repository](https://litesql.github.io/helm-charts) to customize the installation;


## Configuration

Set up your environment variables to configure the cluster:

| Environment Variable | Description                                                                 | Default |
|----------------------|-----------------------------------------------------------------------------|---------|
| `PB_ASYNC_PUBLISHER` | Enables asynchronous replication message publishing. Recommended only when using an external NATS server. | false |
| `PB_ASYNC_PUBLISHER_DIR` | Directory path for storing outbox messages used in asynchronous replication. |    |
| `PB_LOCAL_TARGET`    | Specifies the service URL to redirect requests when this node is the leader. Useful for enabling leader election. | |
| `PB_NAME`            | A unique name for the node. Defaults to the system's hostname if not provided. | $HOSTNAME |
| `PB_NATS_PORT`       | Port for the embedded NATS server (use only if running an embedded NATS server). | |
| `PB_NATS_STORE_DIR`  | Directory for storing data for the embedded NATS server.                   | /tmp/nats |
| `PB_NATS_CONFIG`     | Path to a NATS configuration file (overrides other NATS settings).         | |
| `PB_REPLICATION_URL` | NATS connection URL for replication (use if connecting to an external NATS server). Example: `nats://localhost:4222`. | |
| `PB_REPLICATION_STREAM` | Stream name for data replication | pb |
| `PB_ROW_IDENTIFY` | Strategy used to identify rows during replication. Options: `rowid` or `full`. | rowid |
| `PB_STATIC_LEADER`| URL target to redirect all writer requests to the cluster leader | |
| `PB_SUPERUSER_EMAIL` | Superuser email created at startup | |
| `PB_SUPERUSER_PASS` | Superuser password created at startup | |

## Usage

### Starting a Cluster

1. Start the first PocketBase HA instance:

    ```sh
    PB_NAME=node1 PB_NATS_PORT=4222 pocketbase-ha serve
    ```

2. Start a second instance in a different directory:

    ```sh
    PB_ASYNC_PUBLISHER=true PB_NAME=node2 PB_REPLICATION_URL=nats://localhost:4222 pocketbase-ha serve --http 127.0.0.1:8091
    ```

> **Note**: You can skip setting the superuser password for the second instance.

### Running a NATS Cluster with docker

To run a NATS cluster using Docker Compose, use the following command:

```sh
docker compose up
```

- Superuser e-mail: test@example.com
- Superuser pass: 1234567890

You can define the superuser password using this command:

```sh
docker compose exec -e PB_NATS_CONFIG="" -e PB_LOCAL_TARGET="" node1 /app/pocketbase-ha superuser upsert EMAIL PASS
```

Access the three nodes using the following address:

- Node1: http://localhost:8090
- Node2: http://localhost:8091
- Node3: http://localhost:8092

> **Tip**: Ensure all nodes are synchronized by verifying the logs or using the PocketBase admin interface.

### Event Hooks on replica nodes

On replica nodes (the nodes that users do not directly interact with), only the following events are triggered by the PocketBase event hooks system:

- OnModelAfterCreateSuccess
- OnModelAfterCreateError
- OnModelAfterUpdateSuccess
- OnModelAfterUpdateError
- OnModelAfterDeleteSuccess
- OnModelAfterDeleteError

### Data replication conflict resolution

By default, **PocketBase HA** uses a last-writer-wins approach for conflict resolution. You can modify the `ChangeSetInterceptor` to implement custom strategies tailored to your needs.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to improve this project.

## License

This project is licensed under the [MIT License](LICENSE).

PB_NAME=node1 PB_REDIRECT_TARGET=http://localhost:8090 PB_REPLICATION_URL=nats://localhost:4222 /tmp/pocketbase-ha serve --http 127.0.0.1:8090
