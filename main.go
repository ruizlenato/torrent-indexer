package main

import (
	"net/http"

	handler "github.com/ruizlenato/torrent-indexer/api"
	"github.com/ruizlenato/torrent-indexer/cache"
)

func main() {
	redis := cache.NewRedis()
	indexers := handler.NewIndexers(redis)

	http.HandleFunc("/", handler.HandlerIndex)
	http.HandleFunc("/indexers/comando_torrents", indexers.HandlerComandoIndexer)

	err := http.ListenAndServe(":7006", nil)
	if err != nil {
		panic(err)
	}
}
