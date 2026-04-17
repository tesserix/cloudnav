package aws

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestParseCaller(t *testing.T) {
	data := []byte(`{"UserId":"AIDA1","Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/alice"}`)
	nodes, err := parseCaller(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("len=%d want 1", len(nodes))
	}
	n := nodes[0]
	if n.ID != "123456789012" || n.Kind != provider.KindAccount {
		t.Errorf("node=%+v", n)
	}
	if n.Meta["arn"] != "arn:aws:iam::123456789012:user/alice" {
		t.Errorf("arn=%q", n.Meta["arn"])
	}
}

func TestParseCallerEmpty(t *testing.T) {
	if _, err := parseCaller([]byte(`{"Account":""}`)); err == nil {
		t.Error("expected error for empty account")
	}
}

func TestParseRegions(t *testing.T) {
	data := []byte(`{"Regions":[{"RegionName":"us-east-1","Endpoint":"ec2.us-east-1.amazonaws.com"},{"RegionName":"eu-west-2","Endpoint":"ec2.eu-west-2.amazonaws.com"}]}`)
	account := provider.Node{ID: "123", Kind: provider.KindAccount}
	nodes, err := parseRegions(data, account)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len=%d want 2", len(nodes))
	}
	if nodes[0].ID != "us-east-1" || nodes[0].Kind != provider.KindRegion {
		t.Errorf("region=%+v", nodes[0])
	}
	if nodes[1].Meta["accountId"] != "123" {
		t.Errorf("accountId=%q", nodes[1].Meta["accountId"])
	}
}

func TestParseResources(t *testing.T) {
	data := []byte(`{"ResourceTagMappingList":[{"ResourceARN":"arn:aws:ec2:us-east-1:123:instance/i-abc"},{"ResourceARN":"arn:aws:s3:::my-bucket"}]}`)
	region := provider.Node{ID: "us-east-1", Kind: provider.KindRegion}
	nodes, err := parseResources(data, region)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len=%d want 2", len(nodes))
	}
	if nodes[0].Name != "i-abc" {
		t.Errorf("[0].Name=%q", nodes[0].Name)
	}
	if nodes[0].State != "ec2" {
		t.Errorf("[0].State (service)=%q", nodes[0].State)
	}
	if nodes[1].Name != "my-bucket" {
		t.Errorf("[1].Name=%q", nodes[1].Name)
	}
	if nodes[1].State != "s3" {
		t.Errorf("[1].State=%q", nodes[1].State)
	}
}

func TestNameFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:ec2:us-east-1:123:instance/i-abc": "i-abc",
		"arn:aws:s3:::my-bucket":                   "my-bucket",
		"arn:aws:lambda:us-east-1:123:function:f":  "f",
	}
	for arn, want := range cases {
		if got := nameFromARN(arn); got != want {
			t.Errorf("nameFromARN(%q)=%q want %q", arn, got, want)
		}
	}
}

func TestResourceTypeFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:ec2:us-east-1:123:instance/i-abc": "instance",
		"arn:aws:iam::123:role/my-role":            "role",
		"arn:aws:lambda:us-east-1:123:function:f":  "function",
		"arn:aws:s3:::my-bucket":                   "",
	}
	for arn, want := range cases {
		if got := resourceTypeFromARN(arn); got != want {
			t.Errorf("resourceTypeFromARN(%q)=%q want %q", arn, got, want)
		}
	}
}

func TestServiceFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:ec2:us-east-1:123:instance/i-abc": "ec2",
		"arn:aws:s3:::my-bucket":                   "s3",
		"arn:aws:lambda:us-east-1:123:function:f":  "lambda",
		"": "",
	}
	for arn, want := range cases {
		if got := serviceFromARN(arn); got != want {
			t.Errorf("serviceFromARN(%q)=%q want %q", arn, got, want)
		}
	}
}

func TestPortalURL(t *testing.T) {
	a := New()
	got := a.PortalURL(provider.Node{ID: "us-east-1", Kind: provider.KindRegion})
	want := "https://us-east-1.console.aws.amazon.com/console/home?region=us-east-1"
	if got != want {
		t.Errorf("PortalURL=%q want %q", got, want)
	}
}
