package pddaddress

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

//go:embed pdd_address_codes.json
var rawCodes []byte

type District struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type City struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Districts []District `json:"districts"`
}

type Province struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Cities []City `json:"cities"`
}

type Match struct {
	ProvinceID   int64  `json:"province_id"`
	ProvinceName string `json:"province_name"`
	CityID       int64  `json:"city_id"`
	CityName     string `json:"city_name"`
	DistrictID   int64  `json:"district_id"`
	DistrictName string `json:"district_name"`
	Address      string `json:"address"`
}

var provinces []Province

func init() {
	var document struct {
		Complete bool       `json:"complete"`
		Data     []Province `json:"data"`
	}
	if err := json.Unmarshal(rawCodes, &document); err != nil || !document.Complete {
		panic("invalid embedded PDD address codes")
	}
	provinces = document.Data
}

func compact(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
}

// Resolve only returns an unambiguous district match. It deliberately rejects
// generic "其他区" entries because sending those IDs can route an order wrongly.
func Resolve(cityHint, fullAddress string) (Match, error) {
	haystack := compact(cityHint + fullAddress)
	if haystack == "" {
		return Match{}, errors.New("收货地址为空")
	}
	matches := make([]Match, 0, 2)
	for _, province := range provinces {
		for _, city := range province.Cities {
			for _, district := range city.Districts {
				if district.Name == "" || strings.Contains(district.Name, "其他区") || !strings.Contains(haystack, compact(district.Name)) {
					continue
				}
				matches = append(matches, Match{ProvinceID: province.ID, ProvinceName: province.Name, CityID: city.ID, CityName: city.Name, DistrictID: district.ID, DistrictName: district.Name})
			}
		}
	}
	if len(matches) == 0 {
		return Match{}, errors.New("无法匹配拼多多省市区编码")
	}
	selected := matches[0]
	for _, candidate := range matches[1:] {
		if candidate.DistrictID != selected.DistrictID {
			return Match{}, errors.New("地址包含多个可能的行政区，请人工确认")
		}
	}
	detail := compact(fullAddress)
	for _, prefix := range []string{selected.ProvinceName, selected.CityName, selected.DistrictName} {
		detail = strings.TrimPrefix(detail, compact(prefix))
	}
	selected.Address = detail
	return selected, nil
}

// TemporaryPhone changes the fifth digit. The original phone remains on the
// Xianyu order, so restoration never depends on reversing this transformation.
func TemporaryPhone(phone string) (string, error) {
	digits := []rune(strings.TrimSpace(phone))
	if len(digits) != 11 {
		return "", errors.New("手机号必须为 11 位")
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return "", errors.New("手机号必须只包含数字")
		}
	}
	digits[4] = '0' + (digits[4]-'0'+1)%10
	return string(digits), nil
}
