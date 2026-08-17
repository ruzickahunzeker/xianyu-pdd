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

func TestTemporaryPhone(t *testing.T) {
	got, err := TemporaryPhone("13216514040")
	if err != nil {
		t.Fatal(err)
	}
	if got != "13217514040" || got == "13216514040" {
		t.Fatalf("got=%s", got)
	}
}
