package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/felipemarinho97/torrent-indexer/magnet"
	"github.com/felipemarinho97/torrent-indexer/schema"
	goscrape "github.com/felipemarinho97/torrent-indexer/scrape"
)

var comando = IndexerMeta{
	URL:       "https://comando.la/",
	SearchURL: "?s=",
}

var replacer = strings.NewReplacer(
	"janeiro", "01",
	"fevereiro", "02",
	"março", "03",
	"abril", "04",
	"maio", "05",
	"junho", "06",
	"julho", "07",
	"agosto", "08",
	"setembro", "09",
	"outubro", "10",
	"novembro", "11",
	"dezembro", "12",
)

func (i *Indexer) HandlerComandoIndexer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// supported query params: q, season, episode
	q := r.URL.Query().Get("q")

	// URL encode query param
	q = url.QueryEscape(q)
	url := comando.URL
	if q != "" {
		url = fmt.Sprintf("%s%s%s", url, comando.SearchURL, q)
	}

	fmt.Println("URL:>", url)
	resp, err := http.Get(url)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var links []string
	doc.Find("article").Each(func(i int, s *goquery.Selection) {
		// get link from h2.entry-title > a
		link, _ := s.Find("h2.entry-title > a").Attr("href")
		links = append(links, link)
	})

	var itChan = make(chan []IndexedTorrent)
	var errChan = make(chan error)
	indexedTorrents := []IndexedTorrent{}
	for _, link := range links {
		go func(link string) {
			torrents, err := getTorrents(ctx, i, link)
			if err != nil {
				fmt.Println(err)
				errChan <- err
			}
			itChan <- torrents
		}(link)
	}

	for i := 0; i < len(links); i++ {
		select {
		case torrents := <-itChan:
			indexedTorrents = append(indexedTorrents, torrents...)
		case err := <-errChan:
			fmt.Println(err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if len(indexedTorrents) == 0 {
		w.WriteHeader(http.StatusNotFound)
	}
	json.NewEncoder(w).Encode(indexedTorrents)
}

func getTorrents(ctx context.Context, i *Indexer, link string) ([]IndexedTorrent, error) {
	var indexedTorrents []IndexedTorrent
	doc, err := getDocument(ctx, i, link)
	if err != nil {
		return nil, err
	}

	article := doc.Find("article")
	title := strings.Replace(article.Find(".entry-title").Text(), " - Torrent Download", "", -1)
	textContent := article.Find("div.entry-content")
	// div itemprop="datePublished"
	datePublished := strings.TrimSpace(article.Find("div[itemprop=\"datePublished\"]").Text())
	// pattern: 10 de setembro de 2021
	re := regexp.MustCompile(`(\d{2}) de (\w+) de (\d{4})`)
	matches := re.FindStringSubmatch(datePublished)
	var date time.Time
	if len(matches) > 0 {
		day := matches[1]
		month := matches[2]
		year := matches[3]
		datePublished = fmt.Sprintf("%s-%s-%s", year, replacer.Replace(month), day)
		date, err = time.Parse("2006-01-02", datePublished)
		if err != nil {
			return nil, err
		}
	}
	magnets := textContent.Find("a[href^=\"magnet\"]")
	var magnetLinks []string
	magnets.Each(func(i int, s *goquery.Selection) {
		magnetLink, _ := s.Attr("href")
		magnetLinks = append(magnetLinks, magnetLink)
	})

	var audio []schema.Audio
	var ogtitle string
	var imdb string
	var year string
	var quality string
	var size []string
	article.Find("div.entry-content > p").Each(func(i int, s *goquery.Selection) {
		// pattern:
		// Título Traduzido: Fundação
		// Título Original: Foundation
		// IMDb: 7,5
		// Ano de Lançamento: 2023
		// Gênero: Ação | Aventura | Ficção
		// Formato: MKV
		// Qualidade: WEB-DL
		// Áudio: Português | Inglês
		// Idioma: Português | Inglês
		// Legenda: Português
		// Tamanho: –
		// Qualidade de Áudio: 10
		// Qualidade de Vídeo: 10
		// Duração: 59 Min.
		// Servidor: Torrent
		text := s.Text()

		audio = append(audio, findAudioFromText(text)...)
		if strings.Contains(text, "INFORMAÇÕES") {
			ogtitle = findoOgTitleFromText(text)
			imdb = findIMDbFromText(text)
			year = findYearFromText(text)
			quality = findQualityFromText(text)
		}

		size = append(size, findSizesFromText(text)...)
	})

	size = stableUniq(size)

	var chanIndexedTorrent = make(chan IndexedTorrent)

	// for each magnet link, create a new indexed torrent
	for it, magnetLink := range magnetLinks {
		it := it
		go func(it int, magnetLink string) {
			magnet, err := magnet.ParseMagnetUri(magnetLink)
			if err != nil {
				fmt.Println(err)
			}
			releaseTitle := magnet.DisplayName
			episode, season := findEpisodeFromTitle(releaseTitle)
			quality := fmt.Sprintf("%s %s", quality, findResFromTitle(releaseTitle))
			infoHash := magnet.InfoHash.String()
			trackers := magnet.Trackers
			magnetAudio := []schema.Audio{}
			if strings.Contains(strings.ToLower(releaseTitle), "dual") || strings.Contains(strings.ToLower(releaseTitle), "dublado") {
				magnetAudio = append(magnetAudio, audio...)
			} else if len(audio) > 1 {
				// remove portuguese audio, and append to magnetAudio
				for _, a := range audio {
					if a != schema.AudioPortuguese {
						magnetAudio = append(magnetAudio, a)
					}
				}
			} else {
				magnetAudio = append(magnetAudio, audio...)
			}

			peer, seed, err := goscrape.GetLeechsAndSeeds(ctx, i.redis, infoHash, trackers)
			if err != nil {
				fmt.Println(err)
			}

			title := processTitle(title, magnetAudio)

			// if the number of sizes is equal to the number of magnets, then assign the size to each indexed torrent in order
			var mySize string
			if len(size) == len(magnetLinks) {
				mySize = size[it]
			}

			ixt := IndexedTorrent{
				Title:         title,
				OriginalTitle: ogtitle,
				Episode:       episode,
				Season:        season,
				Quality:       quality,
				Details:       link,
				Year:          year,
				IMDb:          imdb,
				Audio:         magnetAudio,
				MagnetLink:    magnetLink,
				Date:          date,
				InfoHash:      infoHash,
				Trackers:      trackers,
				LeechCount:    peer,
				SeedCount:     seed,
				Size:          mySize,
			}
			chanIndexedTorrent <- ixt
		}(it, magnetLink)
	}

	for i := 0; i < len(magnetLinks); i++ {
		it := <-chanIndexedTorrent
		indexedTorrents = append(indexedTorrents, it)
	}

	return indexedTorrents, nil
}

func stableUniq(s []string) []string {
	var uniq []map[string]interface{}
	m := make(map[string]map[string]interface{})
	for i, v := range s {
		m[v] = map[string]interface{}{
			"v": v,
			"i": i,
		}
	}
	// to order by index
	for _, v := range m {
		uniq = append(uniq, v)
	}

	// sort by index
	for i := 0; i < len(uniq); i++ {
		for j := i + 1; j < len(uniq); j++ {
			if uniq[i]["i"].(int) > uniq[j]["i"].(int) {
				uniq[i], uniq[j] = uniq[j], uniq[i]
			}
		}
	}

	// get only values
	var uniqValues []string
	for _, v := range uniq {
		uniqValues = append(uniqValues, v["v"].(string))
	}

	return uniqValues
}
func findEpisodeFromTitle(title string) (season string, episode string) {
	re := regexp.MustCompile(`S(\d+)E(\d+)`)
	episodeMatch := re.FindStringSubmatch(title)
	if len(episodeMatch) > 0 {
		season = episodeMatch[1]
		episode = episodeMatch[2]
	}

	return season, episode
}
func findResFromTitle(title string) (res string) {
	if strings.Contains(title, "720p") {
		res = "720p"
	}
	if strings.Contains(title, "1080p") {
		res = "1080p"
	}
	if strings.Contains(title, "2160p") || strings.Contains(title, "4k") {
		res = "2160p"
	}

	return res
}

func findQualityFromText(text string) (quality string) {
	re := regexp.MustCompile(`Qualidade:[  ](BluRay|WEB\S+)`)
	qualityMatch := re.FindStringSubmatch(text)
	if len(qualityMatch) > 0 {
		quality = qualityMatch[1]
	}

	return quality
}

func findIMDbFromText(text string) (imdb string) {
	re := regexp.MustCompile(`IMDb: (.+\d)`)
	imdbMatch := re.FindStringSubmatch(text)
	if len(imdbMatch) > 0 {
		imdb = imdbMatch[1]
	} else {
		imdb = ""
	}

	return imdb
}

func findoOgTitleFromText(text string) (ogtitle string) {
	re := regexp.MustCompile(`(?i)(T[íi]tulo original|Baixar Filme): (.*)`)
	ogtitleMatch := re.FindStringSubmatch(text)
	if len(ogtitleMatch) > 0 {
		ogtitle = ogtitleMatch[2]
	} else {
		ogtitle = "Not Found"
	}

	return ogtitle
}

func findYearFromText(text string) (year string) {
	re := regexp.MustCompile(`Lançamento:[  ](.*)`)
	yearMatch := re.FindStringSubmatch(text)
	if len(yearMatch) > 0 {
		year = yearMatch[1]
	}

	return year
}

func findAudioFromText(text string) []schema.Audio {
	var audio []schema.Audio
	re := regexp.MustCompile(`(.udio|Idioma):.?(.*)`)
	audioMatch := re.FindStringSubmatch(text)
	if len(audioMatch) > 0 {
		sep := getSeparator(audioMatch[2])
		langs_raw := strings.Split(audioMatch[2], sep)
		for _, lang := range langs_raw {
			lang = strings.TrimSpace(lang)
			a := schema.GetAudioFromString(lang)
			if a != nil {
				audio = append(audio, *a)
			} else {
				fmt.Println("unknown language:", lang)
			}
		}
	}
	return audio
}

func findSizesFromText(text string) []string {
	var sizes []string
	// everything that ends with GB or MB, using ',' or '.' as decimal separator
	re := regexp.MustCompile(`(\d+[\.,]?\d+) ?(GB|MB)`)
	sizesMatch := re.FindAllStringSubmatch(text, -1)
	if len(sizesMatch) > 0 {
		for _, size := range sizesMatch {
			sizes = append(sizes, size[0])
		}
	}
	return sizes
}

func processTitle(title string, a []schema.Audio) string {
	re := regexp.MustCompile(`(?m)(BluRay|WEB-DL).+(0p )(.*)`)
	title = re.ReplaceAllString(title, "")
	title = (regexp.MustCompile(`(?m)Dual.+`)).ReplaceAllString(title, "")

	title = appendAudioISO639_2Code(title, a)

	return title
}

func appendAudioISO639_2Code(title string, a []schema.Audio) string {
	if len(a) > 0 {
		audio := []string{}
		for _, lang := range a {
			audio = append(audio, lang.String())
		}
		title = fmt.Sprintf("%s(%s)", title, strings.Join(audio, ", "))
	}
	return title
}

func getSeparator(s string) string {
	if strings.Contains(s, "|") {
		return "|"
	} else if strings.Contains(s, ",") {
		return ","
	}
	return " "
}

func getDocument(ctx context.Context, i *Indexer, link string) (*goquery.Document, error) {
	// try to get from redis first
	docCache, err := i.redis.Get(ctx, link)
	if err == nil {
		return goquery.NewDocumentFromReader(ioutil.NopCloser(bytes.NewReader(docCache)))
	}

	resp, err := http.Get(link)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// set cache
	err = i.redis.Set(ctx, link, body)
	if err != nil {
		fmt.Println(err)
	}

	doc, err := goquery.NewDocumentFromReader(ioutil.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return nil, err
	}

	return doc, nil
}
