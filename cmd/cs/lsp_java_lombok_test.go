package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "codesight/pkg/config"
)

func writeLombokTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newLombokTestHome(t *testing.T, gradleVersions []string, mavenVersions []string) string {
	t.Helper()
	home := t.TempDir()
	for _, version := range gradleVersions {
		jar := filepath.Join(
			home, ".gradle", "caches", "modules-2", "files-2.1",
			"org.projectlombok", "lombok", version, "somehash", "lombok-"+version+".jar",
		)
		writeLombokTestFile(t, jar, "jar")
	}
	for _, version := range mavenVersions {
		jar := filepath.Join(
			home, ".m2", "repository", "org", "projectlombok", "lombok", version, "lombok-"+version+".jar",
		)
		writeLombokTestFile(t, jar, "jar")
	}
	return home
}

func TestJavaProjectUsesLombokDetectsBuildFiles(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{name: "root gradle", file: "build.gradle"},
		{name: "version catalog", file: "gradle/libs.versions.toml"},
		{name: "buildSrc", file: "buildSrc/build.gradle.kts"},
		{name: "root pom", file: "pom.xml"},
		{name: "module pom", file: "web-application/pom.xml"},
		{name: "module gradle", file: "web-application/build.gradle"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLombokTestFile(t, filepath.Join(root, tc.file), `dependency "org.projectlombok:lombok:1.18.46"`)
			if !javaProjectUsesLombok(root) {
				t.Fatalf("lombok in %s not detected", tc.file)
			}
		})
	}
}

func TestJavaProjectUsesLombokIgnoresProjectsWithoutLombok(t *testing.T) {
	root := t.TempDir()
	writeLombokTestFile(t, filepath.Join(root, "build.gradle"), `dependency "org.slf4j:slf4j-api:2.0.0"`)
	if javaProjectUsesLombok(root) {
		t.Fatal("detected lombok in a project without it")
	}
	if javaProjectUsesLombok("") {
		t.Fatal("detected lombok with empty workspace root")
	}
}

func TestFindLombokJarPicksNewestAcrossCaches(t *testing.T) {
	home := newLombokTestHome(t, []string{"1.18.30", "edge-SNAPSHOT"}, []string{"1.18.46", "1.9.0"})
	t.Setenv("HOME", home)

	jar := findLombokJar()
	if !strings.HasSuffix(jar, filepath.Join("1.18.46", "lombok-1.18.46.jar")) {
		t.Fatalf("jar = %q, want the newest (1.18.46) from the maven cache", jar)
	}
}

func TestFindLombokJarWithoutCachesReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if jar := findLombokJar(); jar != "" {
		t.Fatalf("jar = %q, want empty", jar)
	}
}

func TestJavaLombokAgentArgsAutoDetects(t *testing.T) {
	home := newLombokTestHome(t, []string{"1.18.38"}, nil)
	t.Setenv("HOME", home)

	root := t.TempDir()
	writeLombokTestFile(t, filepath.Join(root, "pom.xml"), "<artifactId>lombok</artifactId>")

	args := javaLombokAgentArgs(root, configpkg.Defaults())
	if len(args) != 1 || !strings.HasPrefix(args[0], "--jvm-arg=-javaagent:") || !strings.HasSuffix(args[0], "lombok-1.18.38.jar") {
		t.Fatalf("args = %#v, want a single --jvm-arg=-javaagent for lombok-1.18.38.jar", args)
	}
}

func TestJavaLombokAgentArgsSkipsNonLombokProjects(t *testing.T) {
	home := newLombokTestHome(t, []string{"1.18.38"}, nil)
	t.Setenv("HOME", home)

	root := t.TempDir()
	writeLombokTestFile(t, filepath.Join(root, "pom.xml"), "<artifactId>guava</artifactId>")

	if args := javaLombokAgentArgs(root, configpkg.Defaults()); args != nil {
		t.Fatalf("args = %#v, want nil for a project without lombok", args)
	}
}

func TestJavaLombokAgentArgsRespectsOff(t *testing.T) {
	home := newLombokTestHome(t, []string{"1.18.38"}, nil)
	t.Setenv("HOME", home)

	root := t.TempDir()
	writeLombokTestFile(t, filepath.Join(root, "pom.xml"), "<artifactId>lombok</artifactId>")

	cfg := configpkg.Defaults()
	cfg.LSP.Java.Lombok = "off"
	if args := javaLombokAgentArgs(root, cfg); args != nil {
		t.Fatalf("args = %#v, want nil when lombok is off", args)
	}
}

func TestJavaLombokAgentArgsUsesExplicitJarPath(t *testing.T) {
	jar := filepath.Join(t.TempDir(), "lombok-custom.jar")
	writeLombokTestFile(t, jar, "jar")

	cfg := configpkg.Defaults()
	cfg.LSP.Java.Lombok = jar
	args := javaLombokAgentArgs(t.TempDir(), cfg)
	want := "--jvm-arg=-javaagent:" + jar
	if len(args) != 1 || args[0] != want {
		t.Fatalf("args = %#v, want [%q]", args, want)
	}
}

func TestJavaLombokAgentArgsSkipsMissingExplicitJar(t *testing.T) {
	cfg := configpkg.Defaults()
	cfg.LSP.Java.Lombok = filepath.Join(t.TempDir(), "missing.jar")
	if args := javaLombokAgentArgs(t.TempDir(), cfg); args != nil {
		t.Fatalf("args = %#v, want nil for a missing explicit jar", args)
	}
}

func TestJavaLombokAgentArgsSkipsWhenArgsAlreadyCarryAgent(t *testing.T) {
	home := newLombokTestHome(t, []string{"1.18.38"}, nil)
	t.Setenv("HOME", home)

	root := t.TempDir()
	writeLombokTestFile(t, filepath.Join(root, "pom.xml"), "<artifactId>lombok</artifactId>")

	cfg := configpkg.Defaults()
	cfg.LSP.Java.Args = []string{"--jvm-arg=-javaagent:/opt/lombok.jar"}
	if args := javaLombokAgentArgs(root, cfg); args != nil {
		t.Fatalf("args = %#v, want nil when lsp.java.args already sets the agent", args)
	}
}
