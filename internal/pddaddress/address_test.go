package pddaddress

import "testing"

func TestResolve(t *testing.T) {
	match, err := Resolve("北京市", "北京市海淀区中关村大街1号")
	if err != nil {
		t.Fatal(err)
	}
	if match.ProvinceID != 2 || match.CityID != 52 || match.DistrictID != 502 || match.Address != "中关村大街1号" {
		t.Fatalf("match=%+v", match)
	}
}

func TestResolveScopesGenericDistrictToExplicitProvinceAndCity(t *testing.T) {
	match, err := Resolve("佛山市", "广东省佛山市南海区丹灶镇金沙城区上林一品六栋三座1001")
	if err != nil {
		t.Fatal(err)
	}
	if match.ProvinceName != "广东省" || match.CityName != "佛山市" || match.DistrictName != "南海区" {
		t.Fatalf("match=%+v", match)
	}
	if match.Address != "丹灶镇金沙城区上林一品六栋三座1001" {
		t.Fatalf("address=%q", match.Address)
	}
}

func TestTemporaryPhone(t *testing.T) {
	got, err := TemporaryPhone("13216514040")
	if err != nil {
		t.Fatal(err)
	}
	if got != "13217514040" || got == "13216514040" {
		t.Fatalf("got=%s", got)
	}
}
