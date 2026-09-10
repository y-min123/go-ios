package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/danielpaulus/go-ios/ios/crashreport"
	"github.com/danielpaulus/go-ios/ios/debugserver"
	"github.com/danielpaulus/go-ios/ios/imagemounter"
	"github.com/danielpaulus/go-ios/ios/instruments"
	"github.com/danielpaulus/go-ios/ios/ostrace"
	"github.com/danielpaulus/go-ios/ios/pcap"
)

func runPCAPCommand(ctx commandContext) {
	p, _ := ctx.Args.String("--process")
	i, _ := ctx.Args.Int("--pid")
	pcap.Pid = int32(i)
	pcap.ProcName = p
	err := pcap.Start(ctx.Device)
	if err != nil {
		exitIfError("pcap failed", err)
	}
}

func runDproxyCommand(ctx commandContext) {
	binaryMode, _ := ctx.Args.Bool("--binary")
	startDebugProxy(ctx.Device, binaryMode)
}

func runSyslogCommand(ctx commandContext) {
	parse, _ := ctx.Args.Bool("--parse")
	runSyslog(ctx.Device, parse)
}

func runOSTraceCommand(ctx commandContext) {
	pidStr, _ := ctx.Args.String("--pid")
	processName, _ := ctx.Args.String("--process")
	levelStr, _ := ctx.Args.String("--level")
	subsystem, _ := ctx.Args.String("--subsystem")
	match, _ := ctx.Args.String("--match")
	exclude, _ := ctx.Args.String("--exclude")
	pid := -1
	if pidStr != "" {
		var err error
		pid, err = strconv.Atoi(pidStr)
		exitIfError("invalid --pid value", err)
	}
	levelFilter, err := ostrace.ParseLevelFilter(levelStr)
	exitIfError("invalid --level value", err)
	clientFilter := ostrace.ClientFilter{
		Levels:    levelFilter.ClientLevels,
		Subsystem: subsystem,
		Match:     match,
		Exclude:   exclude,
	}
	follow, _ := ctx.Args.Bool("--follow")
	runOsTrace(ctx.Device, pid, processName, levelFilter.MessageFilter, levelFilter.StreamFlags, clientFilter, follow)
}

func runCrashCommand(ctx commandContext) {
	if ls, _ := ctx.Args.Bool("ls"); ls {
		pattern, err := ctx.Args.String("<pattern>")
		if err != nil || pattern == "" {
			pattern = "*"
		}
		files, err := crashreport.ListReports(ctx.Device, pattern)
		exitIfError("failed listing crashreports", err)
		fmt.Println(convertToJSONString(map[string]interface{}{"files": files, "length": len(files)}))
	}
	if cp, _ := ctx.Args.Bool("cp"); cp {
		pattern, _ := ctx.Args.String("<srcpattern>")
		target, _ := ctx.Args.String("<target>")
		slog.Debug("cp", "srcpattern", pattern, "target", target)
		err := crashreport.DownloadReports(ctx.Device, pattern, target)
		exitIfError("failed downloading crashreports", err)
	}
	if rm, _ := ctx.Args.Bool("rm"); rm {
		cwd, _ := ctx.Args.String("<cwd>")
		pattern, _ := ctx.Args.String("<pattern>")
		slog.Debug("rm", "cwd", cwd, "pattern", pattern)
		err := crashreport.RemoveReports(ctx.Device, cwd, pattern)
		exitIfError("failed deleting crashreports", err)
	}
}

func runInstrumentsCommand(ctx commandContext) {
	listenerFunc, closeFunc, err := instruments.ListenAppStateNotifications(ctx.Device)
	if err != nil {
		logFatal("failed listening to app state notifications", "error", err)
	}
	go func() {
		for {
			notification, err := listenerFunc()
			if err != nil {
				slog.Error("listener error", "error", err)
				return
			}
			s, _ := json.Marshal(notification)
			fmt.Println(string(s))
		}
	}()
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	err = closeFunc()
	if err != nil {
		slog.Warn("timeout during close", "error", err)
	}
}

func runImageCommand(ctx commandContext) {
	udid := ctx.Device.Properties.SerialNumber
	if list, _ := ctx.Args.Bool("list"); list {
		listMountedImages(ctx.Device)
	}

	imagePath, _ := ctx.Args.String("--path")
	auto, _ := ctx.Args.Bool("auto")
	if auto {
		basedir, _ := ctx.Args.String("--basedir")
		if basedir == "" {
			basedir = "./devimages"
		}

		// 下载和挂载失败必须以非零退出码结束，否则调用方无法区分成功与失败
		var err error
		imagePath, err = imagemounter.DownloadImageFor(ctx.Device, basedir)
		if err != nil {
			logFatal("failed downloading developer disk image", "basedir", basedir, "udid", udid, "err", err)
		}

		slog.Info("success downloaded developer disk image", "basedir", basedir, "udid", udid, "imagePath", imagePath)
	}

	mount, _ := ctx.Args.Bool("mount")
	if mount || auto {
		err := imagemounter.MountImage(ctx.Device, imagePath)
		if err != nil {
			// auto 模式下的镜像是自动下载的缓存，挂载失败可能是缓存内容损坏，
			// 丢弃后下次连接会重新下载。--path 指定的镜像是用户自己的文件，不能删。
			if auto {
				if discardErr := imagemounter.DiscardCachedImage(imagePath); discardErr != nil {
					slog.Warn("failed discarding cached developer disk image", "imagePath", imagePath, "udid", udid, "err", discardErr)
				}
			}
			logFatal("failed mounting developer disk image", "imagePath", imagePath, "udid", udid, "err", err)
		}
		slog.Info("success mounting developer disk image", "imagePath", imagePath, "udid", udid)
	}

	if unmount, _ := ctx.Args.Bool("unmount"); unmount {
		err := imagemounter.UnmountImage(ctx.Device)
		if err != nil {
			logFatal("failed unmounting developer disk image", "udid", udid, "err", err)
		}
		slog.Info("success unmounting developer disk image", "udid", udid)
	}
}

func runDebugCommand(ctx commandContext) {
	appPath, _ := ctx.Args.String("<app_path>")
	if appPath == "" {
		logFatal("parameter bundleid and app_path must be specified")
	}
	stopAtEntry, _ := ctx.Args.Bool("--stop-at-entry")
	exitIfError("debug server failed", debugserver.Start(ctx.Device, appPath, stopAtEntry))
}
