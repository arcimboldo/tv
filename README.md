# ezupdate

`ezupdate` is a simple tool to download (using [transmission](https://transmissionbt.com/)) and keep up to date torrent
from eztv.ag

There is a TODO list of features I would like to add, feel free to
issue a PR.

## Configuration

By default `ezupdate` reads file `~/.ezupdate.yaml`. You can specify a
different one using option `-f`. So far the configuration options are:

    transmission:
        user: <transmission user, default: admin>
        password: <transmission password
        url: <transmission url, default: http://localhost:9091
    data:
        default_path: <base directory to download torrents>
    quality:
        - 1080p
        - 720p
        - <regexp used to decide which file is preferred when multiple are available>
    shows:
        - <list of shows you want to keep track of, automatically
        managed>
        

## Transmission

You have to enable Transmission's remote access:

* open preferences panel
* select "remote" tab
* check "Allow remote access"
* set a safe password

The password must be then set in `~/.ezupdate.yaml` configuration
file.

## Usage

There are three modes of operation:

* `-list`  list either all shows currently tracker or all
  shows available on eztv

* `-show` display information on a show and its available episodes,
  and optionally download shows using torrent
  
* `-update-all` check if there is any new episode for each one of the
  tracked show and add them to transmission
  
## -show option

The `-show` option display informations on a show. It also displays
all the available episodes and mark those that you already
downloaded. To know if a file was downloaded `ezupdate` uses an
heuristic based on the file names, it does not keep any extra
database. `ezupdate` expects files to be in `default_path` in a folder
named after the show itself.

If you run `-show` with `-update` option ezupdate will download all
latest episodes (from the last episode you downloaded)

You can add a single episode with option `-add` followed by either the
title or the torrent url

You can also download all episodes of a show by running `-update -all`
