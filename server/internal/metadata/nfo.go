package metadata

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}
type actor struct {
	Name string `xml:"name"`
	Role string `xml:"role,omitempty"`
}

type movieNFO struct {
	XMLName       xml.Name   `xml:"movie"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	Year          int        `xml:"year,omitempty"`
	Premiered     string     `xml:"premiered,omitempty"`
	Rating        float64    `xml:"rating,omitempty"`
	UniqueIDs     []uniqueID `xml:"uniqueid"`
	Genres        []string   `xml:"genre,omitempty"`
	Studios       []string   `xml:"studio,omitempty"`
	Actors        []actor    `xml:"actor,omitempty"`
	Tags          []string   `xml:"tag,omitempty"`
	LockData      bool       `xml:"lockdata,omitempty"`
}
type tvshowNFO struct {
	XMLName       xml.Name   `xml:"tvshow"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	Year          int        `xml:"year,omitempty"`
	Premiered     string     `xml:"premiered,omitempty"`
	Rating        float64    `xml:"rating,omitempty"`
	UniqueIDs     []uniqueID `xml:"uniqueid"`
	Genres        []string   `xml:"genre,omitempty"`
	Studios       []string   `xml:"studio,omitempty"`
	Actors        []actor    `xml:"actor,omitempty"`
	Tags          []string   `xml:"tag,omitempty"`
	LockData      bool       `xml:"lockdata,omitempty"`
}
type episodeNFO struct {
	XMLName   xml.Name   `xml:"episodedetails"`
	Title     string     `xml:"title"`
	Plot      string     `xml:"plot,omitempty"`
	Aired     string     `xml:"aired,omitempty"`
	Rating    float64    `xml:"rating,omitempty"`
	Season    int        `xml:"season"`
	Episode   int        `xml:"episode"`
	UniqueIDs []uniqueID `xml:"uniqueid"`
	Actors    []actor    `xml:"actor,omitempty"`
	LockData  bool       `xml:"lockdata,omitempty"`
}

func writeNFO(path string, v any) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append([]byte(xml.Header), append(b, '\n')...)
	if old, err := os.ReadFile(path); err == nil && string(old) == string(b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
