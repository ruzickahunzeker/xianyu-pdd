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

// Resolve prefers the first complete province/city/district chain appearing in
// the address. Once a province or city is known, similarly named districts in
// other regions must not make the address ambiguous (for example, "金沙城区"
// must not override an earlier explicit "广东省佛山市南海区").
func Resolve(cityHint, fullAddress string) (Match, error) {
	address := compact(fullAddress)
	hint := compact(cityHint)
	haystack := hint + address
	if haystack == "" {
		return Match{}, errors.New("收货地址为空")
	}
	type candidate struct {
		match Match
		pos   int
		len   int
	}
	matches := make([]candidate, 0, 2)
	provinceScoped := false
	for _, province := range provinces {
		if name := compact(province.Name); name != "" && strings.Contains(haystack, name) {
			provinceScoped = true
			break
		}
	}
	for _, province := range provinces {
		provinceName := compact(province.Name)
		if provinceScoped && !strings.Contains(haystack, provinceName) {
			continue
		}
		cityScoped := false
		for _, city := range province.Cities {
			if name := compact(city.Name); name != "" && strings.Contains(haystack, name) {
				cityScoped = true
				break
			}
		}
		for _, city := range province.Cities {
			cityName := compact(city.Name)
			if cityScoped && !strings.Contains(haystack, cityName) {
				continue
			}
			for _, district := range city.Districts {
				districtName := compact(district.Name)
				if districtName == "" || strings.Contains(district.Name, "其他区") {
					continue
				}
				pos := strings.Index(address, districtName)
				if pos < 0 {
					pos = strings.Index(hint, districtName)
				}
				if pos < 0 {
					continue
				}
				matches = append(matches, candidate{
					match: Match{ProvinceID: province.ID, ProvinceName: province.Name, CityID: city.ID, CityName: city.Name, DistrictID: district.ID, DistrictName: district.Name},
					pos:   pos,
					len:   len([]rune(districtName)),
				})
			}
		}
	}
	if len(matches) == 0 {
		return Match{}, errors.New("无法匹配拼多多省市区编码")
	}
	selected := matches[0]
	for _, candidate := range matches[1:] {
		if candidate.pos < selected.pos || (candidate.pos == selected.pos && candidate.len > selected.len) {
			selected = candidate
		}
	}
	detail := address
	for _, prefix := range []string{selected.match.ProvinceName, selected.match.CityName, selected.match.DistrictName} {
		detail = strings.TrimPrefix(detail, compact(prefix))
	}
	selected.match.Address = detail
	return selected.match, nil
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
