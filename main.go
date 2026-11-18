package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"formigo/internal/appui"
	"formigo/internal/cli"
	"formigo/internal/config"
	"formigo/internal/converter"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	isMobile := runtime.GOOS == "android" || runtime.GOOS == "ios"

	// Headless CLI mode on desktop when invoked with subcommands or --cli / --help
	if !isMobile && cli.IsCLIInvocation(os.Args[1:]) {
		os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
	}

	mygo.App.SetName("Fomigo")

	var (
		mainWin *mygo.Window
		pipe    *converter.Pipeline
		state   *appui.AppState
	)

	openMain := func() {
		if state == nil {
			return
		}
		opts := mygo.WindowOptions{
			Title:           "Fomigo",
			Width:           1180,
			Height:          760,
			MinWidth:        860,
			MinHeight:       560,
			StateKey:        "main",
			BackgroundColor: "light-dark(#eaf0f8, #090e1a)",
			Content:         ui.View(state.View),
		}
		if !isMobile {
			opts.TitleBarStyle = mygo.TitleBarHidden
			opts.TrafficLightPosition = &mygo.Point{X: 16, Y: 16}
			opts.TitleBarHeight = 40
			opts.Vibrancy = mygo.VibrancySidebar
		}
		mainWin = mygo.NewWindow(opts)
		// Accept files passed on the command line on desktop (e.g. "Open With Fomigo")
		if !isMobile && len(os.Args) > 1 {
			state.AddPaths(os.Args[1:])
		}
	}

	mygo.App.OnOpenFile(func(path string) {
		if path == "" {
			return
		}
		if mainWin != nil && state != nil {
			mainWin.Update(func() {
				state.AddPaths([]string{path})
			})
		}
	})

	mygo.App.WhenReady(func() {
		cfgPath := strings.TrimSpace(os.Getenv("FOMIGO_CONFIG_PATH"))
		if cfgPath == "" {
			if userData, err := mygo.App.Path(mygo.PathUserData); err == nil && userData != "" {
				cfgPath = filepath.Join(userData, "preferences.json")
			}
		}

		store := config.LoadStore(cfgPath)
		prefs := store.Get()
		rt := converter.NewRuntime(prefs.EnginePaths)
		var err error
		pipe, err = converter.NewPipeline(rt)
		if err != nil {
			log.Printf("formigo: failed to initialize runtime workspace: %v", err)
			return
		}

		bridge := appui.PlatformBridge{
			IsMobile:     isMobile,
			SystemLocale: mygo.App.Locale(),
			DispatchMain: func(fn func()) {
				if mainWin != nil {
					mainWin.Update(fn)
				} else {
					mygo.RunOnMain(fn)
				}
			},
			PickFiles: func(defaultDir string, done func(paths []string)) {
				go func() {
					paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
						Parent:      mainWin,
						DefaultPath: defaultDir,
						Multiple:    true,
					})
					if err == nil && len(paths) > 0 && mainWin != nil {
						mainWin.Update(func() { done(paths) })
					}
				}()
			},
			PickFolder: func(defaultDir string, done func(dir string)) {
				go func() {
					paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
						Parent:      mainWin,
						DefaultPath: defaultDir,
						Directory:   true,
					})
					if err == nil && len(paths) > 0 && mainWin != nil {
						mainWin.Update(func() { done(paths[0]) })
					}
				}()
			},
			SaveFile: func(defaultPath string, ext string, done func(path string)) {
				go func() {
					chosen, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
						Parent:            mainWin,
						DefaultPath:       defaultPath,
						CreateDirectories: true,
						Filters: []mygo.FileFilter{
							{Name: ext, Extensions: []string{ext}},
						},
					})
					if err == nil && chosen != "" && mainWin != nil {
						mainWin.Update(func() { done(chosen) })
					}
				}()
			},
			SaveCopy: func(srcPath string, suggestedName string, done func(saved string, err error)) {
				go func() {
					saved, err := mygo.Dialog.SaveCopy(srcPath, suggestedName)
					if mainWin != nil && done != nil {
						mainWin.Update(func() { done(saved, err) })
					}
				}()
			},
			OpenPath: func(path string) {
				go func() { _ = mygo.Shell.OpenPath(path) }()
			},
			RevealPath: func(path string) {
				go func() { mygo.Shell.ShowItemInFolder(path) }()
			},
			SetProgress: func(value float64, active bool, hasError bool) {
				if mainWin == nil || isMobile {
					return
				}
				if !active {
					mainWin.SetProgressBar(mygo.ProgressBar{})
					return
				}
				st := mygo.ProgressNormal
				if hasError {
					st = mygo.ProgressError
				} else if value <= 0.02 {
					st = mygo.ProgressIndeterminate
				}
				mainWin.SetProgressBar(mygo.ProgressBar{
					State: st,
					Value: value,
				})
			},
		}

		state = appui.NewAppState(store, pipe, bridge)
		openMain()
	})

	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" && !isMobile {
			mygo.App.Quit()
		}
	})

	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && !isMobile {
			openMain()
		}
	})

	mygo.App.OnQuit(func() {
		if state != nil {
			state.CancelAll()
		}
		if pipe != nil {
			_ = pipe.Close()
		}
	})

	if err := mygo.App.Run(); err != nil {
		if pipe != nil {
			_ = pipe.Close()
		}
		log.Fatal(err)
	}
}
