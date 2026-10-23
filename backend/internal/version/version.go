package version

var (
	Version = "1.1.29"
	Repo    = "etuos/CLICD_NEW"
)

func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
