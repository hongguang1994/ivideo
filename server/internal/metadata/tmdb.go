package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type tmdbClient struct {
	token, apiBase, imageBase string
	http                      *http.Client
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type tmdbCompany struct {
	Name string `json:"name"`
}
type tmdbCast struct {
	Name      string `json:"name"`
	Character string `json:"character"`
}
type tmdbCredits struct {
	Cast []tmdbCast `json:"cast"`
}

type tmdbItem struct {
	ID                  int           `json:"id"`
	Title               string        `json:"title"`
	OriginalTitle       string        `json:"original_title"`
	Name                string        `json:"name"`
	OriginalName        string        `json:"original_name"`
	Overview            string        `json:"overview"`
	ReleaseDate         string        `json:"release_date"`
	FirstAirDate        string        `json:"first_air_date"`
	PosterPath          string        `json:"poster_path"`
	BackdropPath        string        `json:"backdrop_path"`
	VoteAverage         float64       `json:"vote_average"`
	GenreIDs            []int         `json:"genre_ids"`
	Genres              []tmdbGenre   `json:"genres"`
	Networks            []tmdbCompany `json:"networks"`
	ProductionCompanies []tmdbCompany `json:"production_companies"`
	Credits             tmdbCredits   `json:"credits"`
}

type tmdbEpisode struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Overview      string     `json:"overview"`
	AirDate       string     `json:"air_date"`
	SeasonNumber  int        `json:"season_number"`
	EpisodeNumber int        `json:"episode_number"`
	VoteAverage   float64    `json:"vote_average"`
	StillPath     string     `json:"still_path"`
	GuestStars    []tmdbCast `json:"guest_stars"`
}

func newTMDb(token string) *tmdbClient {
	return &tmdbClient{token: token, apiBase: "https://api.themoviedb.org/3", imageBase: "https://image.tmdb.org/t/p/original", http: &http.Client{Timeout: 25 * time.Second}}
}

func (c *tmdbClient) get(ctx context.Context, endpoint string, query url.Values, out any) error {
	u := c.apiBase + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("TMDb 返回 %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *tmdbClient) verify(ctx context.Context) error {
	var out map[string]any
	return c.get(ctx, "/configuration", nil, &out)
}

func (c *tmdbClient) search(ctx context.Context, kind, title string, year int, requireAnimation bool) (tmdbItem, error) {
	results, err := c.searchResults(ctx, kind, title, year)
	if err != nil {
		return tmdbItem{}, err
	}
	if item, ok := pickSearchResult(results, title, year, requireAnimation); ok {
		return item, nil
	}
	if requireAnimation {
		return tmdbItem{}, fmt.Errorf("TMDb 未找到同名动画 %q", title)
	}
	return tmdbItem{}, fmt.Errorf("TMDb 未找到同名作品 %q", title)
}

func (c *tmdbClient) searchResults(ctx context.Context, kind, title string, year int) ([]tmdbItem, error) {
	endpoint := "/search/tv"
	if kind == "movie" {
		endpoint = "/search/movie"
	}
	q := url.Values{"query": {title}, "language": {"zh-CN"}, "include_adult": {"false"}}
	if year > 0 {
		if kind == "movie" {
			q.Set("year", strconv.Itoa(year))
		} else {
			q.Set("first_air_date_year", strconv.Itoa(year))
		}
	}
	var out struct {
		Results []tmdbItem `json:"results"`
	}
	if err := c.get(ctx, endpoint, q, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, fmt.Errorf("TMDb 未找到 %q", title)
	}
	return out.Results, nil
}

const tmdbAnimationGenreID = 16

func pickSearchResult(items []tmdbItem, title string, year int, requireAnimation bool) (tmdbItem, bool) {
	want := normalizeTitle(title)
	var fallback *tmdbItem
	for _, item := range items {
		if requireAnimation && !containsInt(item.GenreIDs, tmdbAnimationGenreID) {
			continue
		}
		// 非动漫资源优先排除同名动画，避免真人版和动画版混淆。
		if !requireAnimation && containsInt(item.GenreIDs, tmdbAnimationGenreID) {
			continue
		}
		for _, candidate := range []string{item.Title, item.OriginalTitle, item.Name, item.OriginalName} {
			if normalizeTitle(candidate) == want {
				itemYear := yearOf(item.ReleaseDate)
				if itemYear == 0 {
					itemYear = yearOf(item.FirstAirDate)
				}
				if year > 0 && itemYear == year {
					return item, true
				}
				if fallback == nil {
					copy := item
					fallback = &copy
				}
				break
			}
		}
	}
	// 有年份但候选没有可用日期时仍允许命中；如果候选日期明确且年份不符，则拒绝，避免生成明显错误的 NFO。
	if fallback != nil {
		itemYear := yearOf(fallback.ReleaseDate)
		if itemYear == 0 {
			itemYear = yearOf(fallback.FirstAirDate)
		}
		if year == 0 || itemYear == 0 {
			return *fallback, true
		}
	}
	return tmdbItem{}, false
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (c *tmdbClient) details(ctx context.Context, kind string, id int) (tmdbItem, error) {
	endpoint := "/tv/" + strconv.Itoa(id)
	if kind == "movie" {
		endpoint = "/movie/" + strconv.Itoa(id)
	}
	var out tmdbItem
	err := c.get(ctx, endpoint, url.Values{"language": {"zh-CN"}, "append_to_response": {"credits"}}, &out)
	return out, err
}

func (c *tmdbClient) season(ctx context.Context, showID, season int) ([]tmdbEpisode, error) {
	var out struct {
		Episodes []tmdbEpisode `json:"episodes"`
	}
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), url.Values{"language": {"zh-CN"}}, &out)
	return out.Episodes, err
}

func (c *tmdbClient) download(ctx context.Context, imagePath, dst string) error {
	if imagePath == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.imageBase+imagePath, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载海报返回 %d", res.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer(" ", "", ".", "", "-", "", "_", "", "·", "", ":", "", "：", "")
	return r.Replace(s)
}
