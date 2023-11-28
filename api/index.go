package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/felipemarinho97/torrent-indexer/cache"
	"github.com/felipemarinho97/torrent-indexer/schema"
)

type Indexer struct {
	redis *cache.Redis
}

type IndexerMeta struct {
	URL       string
	SearchURL string
}

type IndexedTorrent struct {
	Title         string         `json:"title"`
	OriginalTitle string         `json:"original_title"`
	Quality       string         `json:"quality,omitempty"`
	Details       string         `json:"details"`
	IMDb          string         `json:"imdb,omitempty"`
	Year          string         `json:"year"`
	Audio         []schema.Audio `json:"audio"`
	MagnetLink    string         `json:"magnet_link"`
	Date          time.Time      `json:"date"`
	InfoHash      string         `json:"info_hash"`
	Trackers      []string       `json:"trackers"`
	Size          string         `json:"size"`
	LeechCount    int            `json:"leech_count,omitempty"`
	SeedCount     int            `json:"seed_count,omitempty"`
}

func NewIndexers(redis *cache.Redis) *Indexer {
	return &Indexer{
		redis: redis,
	}
}

func HandlerIndex(w http.ResponseWriter, r *http.Request) {
	currentTime := time.Now().Format(time.RFC850)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"time": currentTime,
		"endpoints": map[string]interface{}{
			"/indexers/comando_torrents": map[string]interface{}{
				"method":      "GET",
				"description": "Indexer for comando torrents",
				"query_params": map[string]string{
					"q": "search query",
				},
			},
		},
	})
}
