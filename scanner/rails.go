package scanner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func configureRails(sourceDir string) (*SourceInfo, error) {
	if !checksPass(sourceDir, dirContains("Gemfile", "rails")) {
		return nil, nil
	}

	s := &SourceInfo{
		Files:  templates("templates/rails/standard"),
		Family: "Rails",
		Port:   8080,
		Statics: []Static{
			{
				GuestPath: "/app/public",
				UrlPrefix: "/",
			},
		},
		PostgresInitCommands: []InitCommand{
			{
				Command:     "bundle",
				Args:        []string{"add", "pg"},
				Description: "Adding the 'pg' gem for Postgres database support",
				Condition:   !checksPass(sourceDir, dirContains("Gemfile", "pg")),
			},
		},
		ReleaseCmd: "bin/rails fly:release",
		Env: map[string]string{
			"SERVER_COMMAND": "bin/rails fly:server",
			"PORT":           "8080",
		},
	}

	var rubyVersion string
	var bundlerVersion string
	var nodeVersion string = "16.17.0"

	out, err := exec.Command("node", "-v").Output()

	if err == nil {
		nodeVersion = strings.TrimSpace(string(out))
		if nodeVersion[:1] == "v" {
			nodeVersion = nodeVersion[1:]
		}
	}

	rubyVersion, err = extractRubyVersion("Gemfile", ".ruby_version")

	if err != nil || rubyVersion == "" {
		rubyVersion = "3.1.2"

		out, err := exec.Command("ruby", "-v").Output()
		if err == nil {

			version := strings.TrimSpace(string(out))
			re := regexp.MustCompile(`ruby (?P<version>[\d.]+)`)
			m := re.FindStringSubmatch(version)

			for i, name := range re.SubexpNames() {
				if len(m) > 0 && name == "version" {
					rubyVersion = m[i]
				}
			}
		}
	}

	bundlerVersion, err = extractBundlerVersion("Gemfile.lock")

	if err != nil || bundlerVersion == "" {
		bundlerVersion = "2.3.21"

		out, err := exec.Command("bundle", "-v").Output()
		if err == nil {

			version := strings.TrimSpace(string(out))
			re := regexp.MustCompile(`Bundler version (?P<version>[\d.]+)`)
			m := re.FindStringSubmatch(version)

			for i, name := range re.SubexpNames() {
				if len(m) > 0 && name == "version" {
					bundlerVersion = m[i]
				}
			}
		}
	}

	s.BuildArgs = map[string]string{
		"RUBY_VERSION":    rubyVersion,
		"BUNDLER_VERSION": bundlerVersion,
		"NODE_VERSION":    nodeVersion,
	}

	// master.key comes with Rails apps from v5.2 onwards, but may not be present
	// if the app does not use Rails encrypted credentials.  Rails v6 added
	// support for multi-environment credentials.  Use the Rails searching
	// sequence for production credentials to determine the RAILS_MASTER_KEY.
	masterKey, err := os.ReadFile("config/credentials/production.key")
	if err != nil {
		masterKey, err = os.ReadFile("config/master.key")
	}

	if err == nil {
		s.Secrets = []Secret{
			{
				Key:   "RAILS_MASTER_KEY",
				Help:  "Secret key for accessing encrypted credentials",
				Value: string(masterKey),
			},
		}
	}

	rake := strings.TrimSpace(`
# commands used to deploy a Rails application
namespace :fly do
  # BUILD step:
  #  - changes to the fs make here DO get deployed
  #  - NO access to secrets, volumes, databases
  #  - Failures here prevent deployment
  task :build => 'assets:precompile'

  # RELEASE step:
  #  - changes to the fs make here are DISCARDED
  #  - full access to secrets, databases
  #  - failures here prevent deployment
  task :release => 'db:migrate'

  # SERVER step:
  #  - changes to the fs make here are deployed
  #  - full access to secrets, databases
  #  - failures here result in VM being stated, shutdown, and rolled back
  #    to last successful deploy (if any).
  task :server do
    sh 'bin/rails server'
  end
end
`)

	_, err = os.Stat("lib/tasks/fly.rake")
	if errors.Is(err, os.ErrNotExist) {
		os.WriteFile("lib/tasks/fly.rake", []byte(rake), 0o600)
	}

	s.SkipDeploy = true
	s.DeployDocs = fmt.Sprintf(`
Your Rails app is prepared for deployment. Production will be set up with these versions of core runtime packages:

Ruby %s
Bundler %s
NodeJS %s

You can configure these in the [build] section in the generated fly.toml.

Ruby versions available are: 3.1.2, 3.0.4, and 2.7.6. Learn more about the chosen Ruby stack, Fullstaq Ruby, here: https://github.com/evilmartians/fullstaq-ruby-docker.
We recommend using the highest patch level for better security and performance.

For the other packages, specify any version you need.

If you need custom packages installed, or have problems with your deployment build, you may need to edit the Dockerfile
for app-specific changes. If you need help, please post on https://community.fly.io.

Now: run 'fly deploy' to deploy your Rails app.
`, rubyVersion, bundlerVersion, nodeVersion)

	return s, nil
}

func extractRubyVersion(gemfilePath string, rubyVersionPath string) (string, error) {
	gemfileContents, err := os.ReadFile(gemfilePath)

	var version string

	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`ruby \"(?P<version>.+)\"`)
	m := re.FindStringSubmatch(string(gemfileContents))

	for i, name := range re.SubexpNames() {
		if len(m) > 0 && name == "version" {
			version = m[i]
		}
	}

	if version == "" {
		if _, err := os.Stat(rubyVersionPath); err == nil {

			versionString, err := os.ReadFile(rubyVersionPath)
			if err != nil {
				return "", err
			}

			version = string(versionString)
		}
	}

	return version, nil
}

func extractBundlerVersion(gemfileLockPath string) (string, error) {
	gemfileContents, err := os.ReadFile(gemfileLockPath)

	var version string

	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`BUNDLED WITH\n\s{3}(?P<version>.+)\n`)
	m := re.FindStringSubmatch(string(gemfileContents))
	for i, name := range re.SubexpNames() {
		if len(m) > 0 && name == "version" {
			version = m[i]
		}
	}

	return version, nil
}
