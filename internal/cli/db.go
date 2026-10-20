package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"howdy/internal/generate"
	"howdy/internal/scaffold"
	"howdy/internal/tools"
	"howdy/internal/tui"
)

func dbCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Migrate and seed the project's database",
		Long: `Work with the database the project's .env names, as the app would: the
SQLite file in DATABASE_PATH or the PostgreSQL database in DATABASE_URL.`,
	}
	cmd.AddCommand(dbMigrateCommand())
	cmd.AddCommand(dbSeedCommand())
	return cmd
}

func dbMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending migrations (gleam run -m tasks/migrate)",
		Long: `Apply every migration in platform/database.gleam that has not run yet,
without starting the server, by running 'gleam run -m tasks/migrate' in the
project. The app also migrates when it starts.`,
		Example: "  howdy db migrate",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := databaseProject()
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(project.Dir, "src", "tasks", "migrate.gleam")); err != nil {
				return errors.New("this project has no src/tasks/migrate.gleam to run; 'howdy new' writes it for a project with a database")
			}
			return gleamRun(project.Dir, "tasks/migrate")
		},
	}
}

func dbSeedCommand() *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Fill the development database with example records",
		Long: `Migrate the database and run dev/seed.gleam, which inserts the records to
start with, by running 'gleam run -m seed' in the project. Records are
built by the factories in dev/<project>/factories/, one for each record
'howdy create repo', 'controller' and 'pages' make; add calls to them to
the seed function.

Seeding again adds the records again. With --reset, every table is dropped
first, then the database is migrated and seeded as new
('gleam run -m seed/reset'). That deletes all the data in the database
.env names, so never point it at data you want to keep.

Code in dev/ is never part of 'gleam run' or a deployment. A project made
before 'howdy new' wrote dev/seed.gleam and dev/seed/reset.gleam gets them
the first time this runs.`,
		Example: "  howdy db seed\n  howdy db seed --reset",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := databaseProject()
			if err != nil {
				return err
			}
			if err := addSeedFiles(project); err != nil {
				return err
			}
			module, done := "seed", "Seeded the database."
			if reset {
				module, done = "seed/reset", "Dropped every table, then migrated and seeded the database."
			}
			if err := gleamRun(project.Dir, module); err != nil {
				return err
			}
			fmt.Println(tui.Success(done))
			return nil
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "drop every table first, deleting all the data, then migrate and seed")
	return cmd
}

// databaseProject is the enclosing project, which must have a database.
func databaseProject() (*generate.Project, error) {
	dir, err := findProjectDir()
	if err != nil {
		return nil, err
	}
	p, err := generate.Open(dir)
	if err == nil && p.Database == "" {
		err = errors.New("this project has no database: 'howdy new' sets one up unless --no-database is passed")
	}
	return p, err
}

// addSeedFiles writes whichever of dev/seed.gleam and dev/seed/reset.gleam
// the project does not have yet, leaving any it has as they are.
func addSeedFiles(p *generate.Project) error {
	files, err := scaffold.SeedFiles(p.Name, scaffold.Database(p.Database))
	if err != nil {
		return err
	}
	var created []string
	for _, f := range files {
		path := filepath.Join(p.Dir, filepath.FromSlash(f.Path))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", f.Path)
		created = append(created, f.Path)
	}
	if len(created) == 0 {
		return nil
	}
	gleam, err := tools.FindGleam()
	if err != nil {
		return err
	}
	return gleam.Run(p.Dir, append([]string{"format"}, created...)...)
}

// gleamRun runs 'gleam run -m module' in dir, streaming its output.
func gleamRun(dir, module string) error {
	gleam, err := tools.FindGleam()
	if err != nil {
		return err
	}
	cmd := exec.Command(gleam.Path, "run", "-m", module)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gleam run -m %s failed: %w", module, err)
	}
	return nil
}
