package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mikedoty/gocliselect"
	// "github.com/pkg/term"

	migrateplus "github.com/mikedoty/golang-migrate-plus"

	_ "github.com/mikedoty/golang-migrate-plus/database/mysql"
	_ "github.com/mikedoty/golang-migrate-plus/database/postgres"
	_ "github.com/mikedoty/golang-migrate-plus/database/singlestore"
	_ "github.com/mikedoty/golang-migrate-plus/source/file"

	"golang.org/x/exp/slices"
	"golang.org/x/term"

	"github.com/buger/goterm"
)

const APP = "golang-migrate-plus-shell"

func newline() {
	fmt.Print("\r\n")
}

func print(msg string) {
	pieces := strings.Split(msg, " ")
	buffer := ""
	lines := []string{}
	for i := range pieces {
		val := strings.TrimSpace(pieces[i])
		if len(buffer)+len(val) <= 58 {
			buffer += val + " "
		} else {
			lines = append(lines, buffer)
			buffer = val + " "
		}

		if strings.HasSuffix(buffer, "\n") {
			lines = append(lines, buffer)
			buffer = ""
		}
	}

	if len(buffer) > 0 {
		lines = append(lines, buffer)
	}

	for _, line := range lines {
		// goterm package doesn't support all the colors, so
		// just do it on our own
		fmt.Printf("\033[1;30m%s\r\n", line)
	}
}

func showCursor() {
	fmt.Printf("\033[?25h")
}

type Profile struct {
	Name             string `json:"name"`
	ConnectionString string `json:"connection_string"`
	MigrationsPath   string `json:"migrations_path"`
}

func getProfiles() ([]Profile, error) {
	profilesFile := ensureConfigFileExists()
	// _, err := os.Stat("profiles.json")
	_, err := os.Stat(profilesFile)
	if err != nil {
		return nil, err
	}

	jsonBytes, err := os.ReadFile(profilesFile)
	if err != nil {
		return nil, err
	}

	var profiles []Profile
	err = json.Unmarshal(jsonBytes, &profiles)
	if err != nil {
		return nil, err
	}

	return profiles, nil
}

func drawSpinner(label string) chan bool {
	chStop := make(chan bool)

	go func() {
		chars := []string{
			"\u2801",
			"\u2809",
			"\u2819",
			"\u2838",
			"\u2834",
			"\u2826",
			"\u2807",
			"\u280b",
		}
		pos := 0

		for {
			fmt.Print("\r")
			fmt.Print(goterm.Color(chars[pos], goterm.CYAN))

			if pos >= 0 {
				fmt.Printf(" %s", label)
			}

			select {
			case success := <-chStop:
				fmt.Printf("\r")
				if success {
					fmt.Print(goterm.Color("\u2713", goterm.GREEN))
				} else {
					fmt.Print(goterm.Color("\u274c", goterm.RED))
				}
				fmt.Print("\r\n")
				return
			default:
				time.Sleep(100 * time.Millisecond)
				pos++
				if pos >= len(chars) {
					pos = 2
				}
			}
		}
	}()

	return chStop
}

func printSuccess(label string) {
	fmt.Printf(goterm.Color("\u2713", goterm.GREEN) + " " + label + "\r\n")
}

func ensureConfigFileExists() string {
	configFolder, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}

	myConfigFolder := path.Join(configFolder, APP)
	_, err = os.Stat(myConfigFolder)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = os.Mkdir(myConfigFolder, os.ModePerm)
			if err != nil {
				panic(err)
			}
		} else {
			panic(err)
		}
	}

	profilesFile := path.Join(myConfigFolder, "profiles.json")
	_, err = os.Stat(profilesFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = os.WriteFile("./profiles.json", []byte("[]"), 0644)
			if err != nil {
				panic(err)
			}
		} else {
			panic(err)
		}
	}

	return profilesFile
}

func main() {
	// ss, _ := os.Getwd()
	// fmt.Println(ss)
	// return

	// ssss, _ := os.UserConfigDir()
	// fmt.Println(ssss)
	// return

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic: %+v\r\n", r)
		}
	}()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	t := term.NewTerminal(os.Stdin, "some prompt")

	run(t)
	fmt.Printf("\r\n")
}

func run(t *term.Terminal) {
	// panic("LOL")
	// bg := context.Background()
	// ctx, _ := context.WithCancel(bg)

	chInterrupt := make(chan os.Signal, 1)
	signal.Notify(chInterrupt, os.Interrupt)

	var err error
	go func() {
		err = doStuff(t, chInterrupt)
		chInterrupt <- os.Kill
	}()

	defer func() {
		if err != nil {
			fmt.Printf("Error: %+v\r\n", err)
		}
	}()

	for {
		select {
		case val := <-chInterrupt:
			if val == os.Interrupt {
				// fmt.Printf("ctrl+c\r\n\r\n")
			}
			return
		default:
			// x = x
			continue
		}
	}
}

func listProfiles(t *term.Terminal, chInterrupt chan<- os.Signal) (*Profile, error) {
	profiles, err := getProfiles()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			profiles = []Profile{}
		} else {
			panic(err)
		}
	}

	// fmt.Printf("\033[1;30mThis is grey text?\r\n")
	if len(profiles) == 0 {
		print("You have not created any database profiles.")
		newline()
		print("Create a new profile using the picker below.  Note that these profiles will be saved in ./profiles.json.")
		newline()
	}

	menu := gocliselect.NewMenu("Select a database profile")
	for i, profile := range profiles {
		menu.AddItem(profile.Name, fmt.Sprintf("%d", i))
	}

	menu.AddItem("[Create New Profile]", "-1")
	menu.AddItem("[Quit]", "-2")

	choice, err := menu.Display()
	if err != nil {
		chInterrupt <- os.Interrupt
		return nil, err
	}

	if choice == "-1" {
		profile := createProfile(t, chInterrupt)
		if profile != nil {
			profiles = append(profiles, *profile)
			// jsonBytes, err := json.Marshal(profiles)
			jsonBytes, err := json.MarshalIndent(profiles, "", "  ")
			if err != nil {
				panic(err)
			}

			profilesFile := ensureConfigFileExists()
			// err = os.WriteFile("./profiles.json", jsonBytes, 0644)
			err = os.WriteFile(profilesFile, jsonBytes, 0644)
			if err != nil {
				panic(err)
			}
		}

		if profile == nil {
			return nil, io.EOF
		} else {
			return profile, nil
		}
	} else if choice == "-2" {
		chInterrupt <- os.Kill
		return nil, io.EOF
	} else {
		for i := range profiles {
			if fmt.Sprintf("%d", i) == choice {
				return &profiles[i], nil
			}
		}

		chInterrupt <- os.Kill
		return nil, io.EOF
	}
}

func queryUser(prompt string, choices ...string) string {
	menu := gocliselect.NewMenu(prompt)
	for _, choice := range choices {
		menu.AddItem(choice, choice)
	}

	choice, err := menu.Display()
	if err != nil {
		return ""
	}

	return choice
}

func createProfile(t *term.Terminal, chInterrupt chan<- os.Signal) *Profile {
	profile := &Profile{}

	newline()
	print("The name of the profile is displayed in the picker when you run the program.  It should be unique.")
	newline()
	print("Example: Local Postgres")
	newline()

	for {
		t.SetPrompt(goterm.Color(goterm.Bold("Profile Name"), goterm.CYAN) + ": ")
		name, err := t.ReadLine()
		if err != nil {
			chInterrupt <- os.Kill
			return nil
		} else if strings.TrimSpace(name) == "" {
			newline()
			print("Name is required.")
			newline()
		} else {
			profile.Name = name
			break
		}
	}

	newline()
	print("Enter the database connection string that will be sent to the migrate tool.")
	newline()
	print("Profiles are saved in the profiles.json folder alongside the compiled app.  You may prefer to enter throwaway values on the CLI and edit the json file directly afterward.")
	newline()
	print("Example: postgres://postgres:postgres@localhost:5433/alpha?sslmode=disable&x-migrations-history-enabled=true")
	newline()

	for {
		t.SetPrompt(goterm.Color(goterm.Bold("Connection String"), goterm.CYAN) + ": ")
		connStr, err := t.ReadLine()
		if err != nil {
			chInterrupt <- os.Kill
			return nil
		} else if strings.TrimSpace(connStr) == "" {
			newline()
			print("Connection String is required.")
			newline()
		} else {
			profile.ConnectionString = connStr
			break
		}
	}

	newline()
	print("Enter the filepath where the migrations are stored.")
	newline()
	print("Example: file:///home/myusername/project/db/?x-migrations-path=migrations")
	newline()

	for {
		t.SetPrompt(goterm.Color(goterm.Bold("Filepath"), goterm.CYAN) + ": ")
		filesStr, err := t.ReadLine()
		if err != nil {
			chInterrupt <- os.Kill
			return nil
		} else if strings.TrimSpace(filesStr) == "" {
			newline()
			print("Filepath is required.")
			newline()
		} else {
			profile.MigrationsPath = filesStr
			break
		}
	}

	return profile
}

func getMissingVersions(profile *Profile, appliedVersions []int) []int {
	if len(appliedVersions) == 0 {
		// Can't determine missing versions if nothing is
		// on record as having been applied in db table
		return []int{}
	}

	folderPath := strings.ReplaceAll(profile.MigrationsPath, "file://", "")

	pieces := strings.Split(folderPath, "?")
	folderPath = pieces[0]

	if len(pieces) > 1 {
		rx := regexp.MustCompile("x-migrations-path=([^&]*)")
		results := rx.FindStringSubmatch(pieces[1])
		if len(results) > 1 {
			folderPath = path.Join(folderPath, results[1])
		}
	}

	files, err := os.ReadDir(folderPath)
	if err != nil {
		panic(err)
	}

	// Only consider migrations that are within bounds of all applied
	// versions.  If a newer one is missing, it should be applied
	// via m.Up() - it's not missing, it just hasn't been run yet...
	minAppliedVersion := appliedVersions[0]
	maxAppliedVersion := appliedVersions[len(appliedVersions)-1]

	history := map[string]bool{}
	versions := []int{}
	for _, f := range files {
		filename := f.Name()
		if !strings.HasSuffix(filename, ".up.sql") {
			continue
		} else if _, exists := history[f.Name()]; exists {
			continue
		}

		version, err := strconv.Atoi(f.Name()[0:len("20241212001122")])
		if err != nil {
			panic(err)
		}

		if version > minAppliedVersion && version < maxAppliedVersion {
			if !slices.Contains(appliedVersions, version) {
				versions = append(versions, version)
			}
		}
	}

	return versions
}

func getClosestPreviousAppliedVersion(appliedVersions []int, missingVersion int) int {
	for i := len(appliedVersions) - 1; i >= 0; i-- {
		if appliedVersions[i] < missingVersion {
			return appliedVersions[i]
		}
	}

	return appliedVersions[0]
}

func doStuff(t *term.Terminal, chInterrupt chan<- os.Signal) error {
	postscripts := []string{}

	profile, err := listProfiles(t, chInterrupt)
	if profile == nil || err != nil {
		chInterrupt <- os.Kill
		return err
	}

	newline()

	mp, err := migrateplus.New(profile.MigrationsPath, profile.ConnectionString)
	if err != nil {
		return err
	}

	appliedVersions, err := mp.ListAppliedVersions()
	if err != nil {
		return err
	}

	chStopCheckMissing := drawSpinner("checking for missing migrations...")
	missingVersions := getMissingVersions(profile, appliedVersions)
	chStopCheckMissing <- true

	newline()

	// xx := 5
	// if xx > 0 {
	// 	missingVersions = []int{20240830112233, 20240830112234}
	// }

	if len(missingVersions) == 0 {
		printSuccess("No missing versions found")
		newline()
	} else {
		print(fmt.Sprintf("Found %d missing migration(s).", len(missingVersions)))
		newline()
		print("You can attempt to apply these retroactively right now, or you can manually copy and paste them separately.")
		newline()

		resp := queryUser("Automatically apply missing migrations now?", "Yes", "No")
		if resp == "Yes" {
			newline()
			for _, version := range missingVersions {
				chStopApplyMissingVersion := drawSpinner(fmt.Sprintf("applying missing version %d...", version))

				curVersion, _, err := mp.Version()
				if err != nil {
					return err
				}

				appliedVersions, err := mp.ListAppliedVersions()
				if err != nil {
					return err
				}

				prevVersion := getClosestPreviousAppliedVersion(appliedVersions, version)

				err = mp.Force(prevVersion)
				if err != nil {
					return err
				}

				applyMissingErr := mp.Steps(1)

				err = mp.Force(int(curVersion))
				if err != nil {
					return err
				}

				// This is just here to allow user to admire
				// the "spinner" effect during super-fast migrations
				time.Sleep(250 * time.Millisecond)

				chStopApplyMissingVersion <- (applyMissingErr == nil)
			}
			newline()
		} else {
			newline()
			print("Skipping missed migrations.  Missing migrations will be listed at the end.")
			newline()

			for _, v := range missingVersions {
				postscripts = append(postscripts, fmt.Sprintf("Missing version: %d", v))
			}
		}
	}

	chStopMigrateUp := drawSpinner("applying migrations...")
	err = mp.Up()
	time.Sleep(1 * time.Second)
	chStopMigrateUp <- (err == nil || err == migrateplus.ErrNoChange)
	if err != nil && err != migrateplus.ErrNoChange {
		return err
	}

	if len(postscripts) > 0 {
		newline()
		for _, ps := range postscripts {
			print(ps)
		}
	}

	// all done...
	chInterrupt <- os.Kill
	return nil
}
