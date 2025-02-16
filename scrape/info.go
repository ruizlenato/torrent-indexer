package goscrape

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruizlenato/torrent-indexer/cache"
)

type peers struct {
	Seeders  int `json:"seed"`
	Leechers int `json:"leech"`
}

func getPeersFromCache(ctx context.Context, r *cache.Redis, infoHash string) (int, int, error) {
	// get peers and seeds from redis first
	peersCache, err := r.Get(ctx, infoHash)
	if err == nil {
		var peers peers
		err = json.Unmarshal(peersCache, &peers)
		if err != nil {
			return 0, 0, err
		}
		return peers.Leechers, peers.Seeders, nil
	}
	return 0, 0, err
}

func setPeersToCache(ctx context.Context, r *cache.Redis, infoHash string, peer, seed int) error {
	peers := peers{
		Seeders:  seed,
		Leechers: peer,
	}
	peersJSON, err := json.Marshal(peers)
	if err != nil {
		return err
	}
	err = r.SetWithExpiration(ctx, infoHash, peersJSON, 24*time.Hour)
	if err != nil {
		return err
	}
	return nil
}

func GetLeechsAndSeeds(ctx context.Context, r *cache.Redis, infoHash string, trackers []string) (int, int, error) {
	leech, seed, err := getPeersFromCache(ctx, r, infoHash)
	if err != nil {
		fmt.Println("unable to get peers from cache for infohash:", infoHash)
	} else {
		fmt.Println("get from cache> leech:", leech, "seed:", seed)
		return leech, seed, nil
	}

	var peerChan = make(chan peers)
	var errChan = make(chan error)

	for _, tracker := range trackers {
		go func(tracker string) {
			// get peers and seeds from redis first
			scraper, err := New(tracker)
			if err != nil {
				errChan <- err
				return
			}

			scraper.SetTimeout(500 * time.Millisecond)

			// get peers and seeds from redis first
			res, err := scraper.Scrape([]byte(infoHash))
			if err != nil {
				errChan <- err
				return
			}

			peerChan <- peers{
				Seeders:  int(res[0].Seeders),
				Leechers: int(res[0].Leechers),
			}
		}(tracker)
	}

	var peer peers
	for i := 0; i < len(trackers); i++ {
		select {
		case peer = <-peerChan:
			setPeersToCache(ctx, r, infoHash, peer.Leechers, peer.Seeders)
			return peer.Leechers, peer.Seeders, nil
		case err := <-errChan:
			fmt.Println(err)
		}
	}

	return 0, 0, fmt.Errorf("unable to get peers from trackers")
}
