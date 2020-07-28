// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxydatasource

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/licensecheck"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/derrors"
	"golang.org/x/pkgsite/internal/licenses"
	"golang.org/x/pkgsite/internal/proxy"
	"golang.org/x/pkgsite/internal/testing/sample"
	"golang.org/x/pkgsite/internal/testing/testhelper"
	"golang.org/x/pkgsite/internal/version"
)

func setup(t *testing.T) (context.Context, *DataSource, func()) {
	t.Helper()
	contents := map[string]string{
		"go.mod":     "module foo.com/bar",
		"LICENSE":    testhelper.MITLicense,
		"baz/baz.go": "//Package baz provides a helpful constant.\npackage baz\nimport \"net/http\"\nconst OK = http.StatusOK",
	}
	testModules := []*proxy.Module{
		{
			ModulePath: "foo.com/bar",
			Version:    "v1.1.0",
			Files:      contents,
		},
		{
			ModulePath: "foo.com/bar",
			Version:    "v1.2.0",
			Files:      contents,
		},
	}
	client, teardownProxy := proxy.SetupTestProxy(t, testModules)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return ctx, New(client), func() {
		teardownProxy()
		cancel()
	}
}

var (
	wantLicenseMD  = sample.LicenseMetadata[0]
	wantLicense    = &licenses.License{Metadata: wantLicenseMD}
	wantLicenseMIT = &licenses.License{
		Metadata: &licenses.Metadata{
			Types:    []string{"MIT"},
			FilePath: "LICENSE",
			Coverage: licensecheck.Coverage{
				Percent: 100,
				Match:   []licensecheck.Match{{Name: "MIT", Type: licensecheck.MIT, Percent: 100, End: 1049}},
			},
		},
		Contents: []byte(testhelper.MITLicense),
	}
	wantLicenseBSD = &licenses.License{
		Metadata: &licenses.Metadata{
			Types:    []string{"BSD-0-Clause"},
			FilePath: "qux/LICENSE",
			Coverage: licensecheck.Coverage{
				Percent: 100,
				Match:   []licensecheck.Match{{Name: "BSD-0-Clause", Type: licensecheck.BSD, Percent: 100, End: 633}},
			},
		},
		Contents: []byte(testhelper.BSD0License),
	}
	wantPackage = internal.LegacyPackage{
		Path:              "foo.com/bar/baz",
		Name:              "baz",
		Imports:           []string{"net/http"},
		Synopsis:          "Package baz provides a helpful constant.",
		V1Path:            "foo.com/bar/baz",
		Licenses:          []*licenses.Metadata{wantLicenseMD},
		IsRedistributable: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
	}
	wantModuleInfo = internal.ModuleInfo{
		ModulePath:        "foo.com/bar",
		Version:           "v1.2.0",
		CommitTime:        time.Date(2019, 1, 30, 0, 0, 0, 0, time.UTC),
		VersionType:       version.TypeRelease,
		IsRedistributable: true,
		HasGoMod:          true,
	}
	wantVersionedPackage = &internal.LegacyVersionedPackage{
		LegacyModuleInfo: internal.LegacyModuleInfo{ModuleInfo: wantModuleInfo},
		LegacyPackage:    wantPackage,
	}
	cmpOpts = append([]cmp.Option{
		cmpopts.IgnoreFields(internal.LegacyPackage{}, "DocumentationHTML"),
		cmpopts.IgnoreFields(licenses.License{}, "Contents"),
	}, sample.LicenseCmpOpts...)
)

func TestDataSource_LegacyGetDirectory(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	want := &internal.LegacyDirectory{
		LegacyModuleInfo: internal.LegacyModuleInfo{ModuleInfo: wantModuleInfo},
		Path:             "foo.com/bar",
		Packages:         []*internal.LegacyPackage{&wantPackage},
	}
	got, err := ds.LegacyGetDirectory(ctx, "foo.com/bar", internal.UnknownModulePath, "v1.2.0", internal.AllFields)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		t.Errorf("LegacyGetDirectory diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetImports(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	want := []string{"net/http"}
	got, err := ds.GetImports(ctx, "foo.com/bar/baz", "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		t.Errorf("GetImports diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetPackage_Latest(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetPackage(ctx, "foo.com/bar/baz", internal.UnknownModulePath, internal.LatestVersion)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantVersionedPackage, got, cmpOpts...); diff != "" {
		t.Errorf("GetLatestPackage diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetModuleInfo_Latest(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetModuleInfo(ctx, "foo.com/bar", internal.LatestVersion)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantModuleInfo, got.ModuleInfo, cmpOpts...); diff != "" {
		t.Errorf("GetLatestModuleInfo diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetModuleInfo(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.GetModuleInfo(ctx, "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(&wantModuleInfo, got, cmpOpts...); diff != "" {
		t.Errorf("GetModuleInfo diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetLicenses(t *testing.T) {
	t.Helper()
	testModules := []*proxy.Module{
		{
			ModulePath: "foo.com/bar",
			Version:    "v1.1.0",
			Files: map[string]string{
				"go.mod":  "module foo.com/bar",
				"LICENSE": testhelper.MITLicense,
				"bar.go":  "//Package bar provides a helpful constant.\npackage bar\nimport \"net/http\"\nconst OK = http.StatusOK",

				"baz/baz.go": "//Package baz provides a helpful constant.\npackage baz\nimport \"net/http\"\nconst OK = http.StatusOK",

				"qux/LICENSE": testhelper.BSD0License,
				"qux/qux.go":  "//Package qux provides a helpful constant.\npackage qux\nimport \"net/http\"\nconst OK = http.StatusOK",
			},
		},
	}
	client, teardownProxy := proxy.SetupTestProxy(t, testModules)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ds := New(client)
	teardown := func() {
		teardownProxy()
		cancel()
	}
	defer teardown()

	tests := []struct {
		err        error
		name       string
		fullPath   string
		modulePath string
		want       []*licenses.License
	}{
		{name: "no license dir", fullPath: "foo.com", modulePath: "foo.com/bar", err: derrors.NotFound},
		{name: "invalid dir", fullPath: "foo.com/invalid", modulePath: "foo.com/invalid", err: derrors.NotFound},
		{name: "root dir", fullPath: "foo.com/bar", modulePath: "foo.com/bar", want: []*licenses.License{wantLicenseMIT}},
		{name: "package with no extra license", fullPath: "foo.com/bar/baz", modulePath: "foo.com/bar", want: []*licenses.License{wantLicenseMIT}},
		{name: "package with additional license", fullPath: "foo.com/bar/qux", modulePath: "foo.com/bar", want: []*licenses.License{wantLicenseMIT, wantLicenseBSD}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ds.GetLicenses(ctx, test.fullPath, test.modulePath, "v1.1.0")
			if !errors.Is(err, test.err) {
				t.Fatal(err)
			}

			sort.Slice(got, func(i, j int) bool {
				return got[i].FilePath < got[j].FilePath
			})
			sort.Slice(test.want, func(i, j int) bool {
				return test.want[i].FilePath < test.want[j].FilePath
			})

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("GetLicenses diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDataSource_LegacyGetModuleLicenses(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetModuleLicenses(ctx, "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []*licenses.License{wantLicense}
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		t.Errorf("LegacyGetModuleLicenses diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetPackage(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetPackage(ctx, "foo.com/bar/baz", internal.UnknownModulePath, "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantVersionedPackage, got, cmpOpts...); diff != "" {
		t.Errorf("GetPackage diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetPackageLicenses(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetPackageLicenses(ctx, "foo.com/bar/baz", "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []*licenses.License{wantLicense}
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		t.Errorf("LegacyGetPackageLicenses diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetPackagesInVersion(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetPackagesInModule(ctx, "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []*internal.LegacyPackage{&wantPackage}
	if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
		t.Errorf("GetPackagesInVersion diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetTaggedVersionsForModule(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetTaggedVersionsForModule(ctx, "foo.com/bar")
	if err != nil {
		t.Fatal(err)
	}
	v110 := wantModuleInfo
	v110.Version = "v1.1.0"
	want := []*internal.ModuleInfo{
		&wantModuleInfo,
		&v110,
	}
	ignore := cmpopts.IgnoreFields(internal.ModuleInfo{}, "CommitTime", "VersionType", "IsRedistributable", "HasGoMod")
	if diff := cmp.Diff(want, got, ignore); diff != "" {
		t.Errorf("LegacyGetTaggedVersionsForPackageSeries diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetTaggedVersionsForPackageSeries(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	// // TODO (rFindley): this shouldn't be necessary.
	// _, err := ds.GetLatestPackage(ctx, "foo.com/bar/baz")
	// if err != nil {
	// 	t.Fatal(err)
	// }
	got, err := ds.LegacyGetTaggedVersionsForPackageSeries(ctx, "foo.com/bar/baz")
	if err != nil {
		t.Fatal(err)
	}
	v110 := wantModuleInfo
	v110.Version = "v1.1.0"
	want := []*internal.ModuleInfo{
		&wantModuleInfo,
		&v110,
	}
	ignore := cmpopts.IgnoreFields(internal.ModuleInfo{}, "CommitTime", "VersionType", "IsRedistributable", "HasGoMod")
	if diff := cmp.Diff(want, got, ignore); diff != "" {
		t.Errorf("LegacyGetTaggedVersionsForPackageSeries diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_LegacyGetModuleInfo(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()
	got, err := ds.LegacyGetModuleInfo(ctx, "foo.com/bar", "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantModuleInfo, got.ModuleInfo, cmpOpts...); diff != "" {
		t.Errorf("LegacyGetModuleInfo diff (-want +got):\n%s", diff)
	}
}

func TestDataSource_GetPathInfo(t *testing.T) {
	ctx, ds, teardown := setup(t)
	defer teardown()

	for _, test := range []struct {
		path, modulePath, version   string
		wantModulePath, wantVersion string
		wantIsPackage               bool
	}{
		{
			path:           "foo.com/bar",
			modulePath:     "foo.com/bar",
			version:        "v1.1.0",
			wantModulePath: "foo.com/bar",
			wantVersion:    "v1.1.0",
			wantIsPackage:  false,
		},
		{
			path:           "foo.com/bar/baz",
			modulePath:     "foo.com/bar",
			version:        "v1.1.0",
			wantModulePath: "foo.com/bar",
			wantVersion:    "v1.1.0",
			wantIsPackage:  true,
		},
		{
			path:           "foo.com/bar/baz",
			modulePath:     internal.UnknownModulePath,
			version:        "v1.1.0",
			wantModulePath: "foo.com/bar",
			wantVersion:    "v1.1.0",
			wantIsPackage:  true,
		},
		{
			path:           "foo.com/bar/baz",
			modulePath:     internal.UnknownModulePath,
			version:        internal.LatestVersion,
			wantModulePath: "foo.com/bar",
			wantVersion:    "v1.2.0",
			wantIsPackage:  true,
		},
	} {
		gotModulePath, gotVersion, gotIsPackage, err := ds.GetPathInfo(ctx, test.path, test.modulePath, test.version)
		if err != nil {
			t.Fatal(err)
		}
		if gotModulePath != test.wantModulePath || gotVersion != test.wantVersion || gotIsPackage != test.wantIsPackage {
			t.Errorf("GetPathInfo(%q, %q, %q) = %q, %q, %t, want %q, %q, %t",
				test.path, test.modulePath, test.version,
				gotModulePath, gotVersion, gotIsPackage,
				test.wantModulePath, test.wantVersion, test.wantIsPackage)
		}
	}
}
