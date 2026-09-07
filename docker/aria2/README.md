# PocketDrive aria2 image

This image installs the distribution-maintained `aria2` and CA packages on
Alpine 3.23. The repository workflow builds amd64 and arm64 images on main,
version tags and every Monday. Weekly builds bypass package caches. Release,
commit and dated scheduled tags are published alongside `latest`; use a digest
when an immutable deployment is needed. Alpine version upgrades still require
a source change before that release reaches end of support.

## Configuration

- `RPC_SECRET` is required. RPC port 6800 is internal to the Compose network.
- `/data` is shared with PocketDrive. Both images currently run as root, with
  umask 022. If setting `user:` explicitly, grant that user write access to
  both `/data` and `/config` in advance, and align PocketDrive permissions.
- `/config/aria2.session` is loaded at startup and saved every 30 seconds and
  on graceful termination. Compose allows 60 seconds for shutdown.
- DHT state uses `/config/dht.dat` and `/config/dht6.dat`; TCP/UDP 6888 is used
  for peer connections and DHT. IPv6 also requires host/container connectivity.
- The image uses `/etc/aria2/aria2.conf`. It does not load the old
  `/config/aria2.conf`, invoke third-party hooks or auto-delete downloads.
- PocketDrive applies concurrency, bandwidth and task options through RPC.
  `PUID`, `PGID`, `UMASK_SET`, `LISTEN_PORT` and `MAX_CONCURRENT_DOWNLOADS`
  from the old image are no longer used.

## Local build and validation

Run from the repository root with Docker and Node 24:

```sh
docker build --pull -f docker/aria2/Dockerfile -t pocketdrive-aria2:test .
node scripts/aria2-smoke.mjs
```

The smoke check uses a disposable container and volumes, verifies RPC auth,
settings, HTTP file integrity, pause/resume across restart and torrent file
selection. It does not test public DHT/tracker reachability or real BT peers.

## Migration from p3terx/aria2-pro

Publish the new image before switching a deployed Compose file. Make the GHCR
package public on its first publication so unauthenticated installs can pull it.
The install script pulls the new image before stopping an old deployment, then
backs up its Compose file and aria2 config under `/opt/pocketdrive/backup/`.

For manual/1Panel upgrades:

1. Pull `ghcr.io/lqlcj/pocketdrive-aria2:latest` first.
2. Stop PocketDrive and aria2 gracefully (60-second timeout).
3. Back up the existing Compose file and `config/aria2` outside `/data`.
4. Replace the aria2 service using the repository Compose example. Keep the
   same `/data`, `/config` mounts and RPC secret. Remove old image variables.
5. Confirm the old config's `save-session` path. The new image expects
   `/config/aria2.session`; copy the saved session there if it used another name.
   Review custom per-task options in that session for old paths or hooks.
6. Start both services. Check logs, existing task GIDs, file selection, and a
   resumed download before discarding the backup. Keep all `.aria2` control files.

Custom trackers, proxy settings, limits and hooks from the old global config
are not imported automatically. Configure supported settings in PocketDrive;
review any additional settings before adding them to the image config.
SQLite history alone cannot restore a missing aria2 session or missing torrent
metadata. Tasks absent from the restored session may need to be submitted again.

To roll back, stop both services, restore the backed-up Compose and aria2 config,
then start the old image. Download files are shared and are not removed by this
migration; avoid running both download engines against the same data concurrently.
