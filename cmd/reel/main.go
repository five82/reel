// Package main provides the CLI entry point for Reel.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/five82/reel/internal/config"
	"github.com/five82/reel/internal/discovery"
	"github.com/five82/reel/internal/logging"
	"github.com/five82/reel/internal/processing"
	"github.com/five82/reel/internal/reporter"
	"github.com/five82/reel/internal/util"
)

const (
	appName    = "reel"
	appVersion = "0.2.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "encode":
		if err := runEncode(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Printf("%s version %s\n", appName, appVersion)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`%s - Video encoding tool

Usage:
  %s <command> [options]

Commands:
  encode    Encode video files to AV1 format
  version   Print version information
  help      Show this help message

Run '%s encode --help' for encode command options.
`, appName, appName, appName)
}

// encodeArgs holds the parsed arguments for the encode command.
type encodeArgs struct {
	inputPath       string
	outputDir       string
	logDir          string
	verbose         bool
	crf             string // Single value or comma-separated triple (SD,HD,UHD)
	preset          uint
	disableAutocrop bool
	noLog           bool
	workers         int
	chunkBuffer     int
	threads         int
}

func runEncode(args []string) error {
	// Get auto-detected defaults for parallel encoding
	defaultWorkers, defaultBuffer := config.AutoParallelConfig()

	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Encode video files to AV1 format.

Usage:
  %s encode [options]

Required:
  -i, --input <PATH>     Input video file or directory containing video files
  -o, --output <PATH>    Output directory (or filename if input is a single file)

Options:
  -l, --log-dir <PATH>   Log directory (defaults to ~/.local/state/reel/logs)
  -v, --verbose          Enable verbose output for troubleshooting

Quality Settings:
  --crf <VALUE>          CRF quality level (0-63, lower=better). Accepts:
                           Single value: --crf 27 (use for all resolutions)
                           Triple: --crf 25,27,29 (SD,HD,UHD)
                         Defaults: SD=%d, HD=%d, UHD=%d
  --preset <0-13>        SVT-AV1 encoder preset. Lower=slower/better. Default: %d

Processing Options:
  --disable-autocrop     Disable automatic black bar crop detection
  --workers <N>          Number of parallel encoder workers. Default: %d (auto)
  --buffer <N>           Extra chunks to buffer in memory. Default: %d (auto)
  --threads <N>          Threads per worker (SVT-AV1 --lp flag). Default: auto
                           Auto mode detects physical cores and SMT, then calculates
                           optimal threads based on resolution. Override if needed.

Output Options:
  --no-log               Disable Reel log file creation
`, appName, config.DefaultCRFSD, config.DefaultCRFHD, config.DefaultCRFUHD, config.DefaultSVTAV1Preset, defaultWorkers, defaultBuffer)
	}

	var ea encodeArgs

	// Required arguments
	fs.StringVar(&ea.inputPath, "i", "", "Input video file or directory")
	fs.StringVar(&ea.inputPath, "input", "", "Input video file or directory")
	fs.StringVar(&ea.outputDir, "o", "", "Output directory")
	fs.StringVar(&ea.outputDir, "output", "", "Output directory")

	// Optional arguments
	fs.StringVar(&ea.logDir, "l", "", "Log directory")
	fs.StringVar(&ea.logDir, "log-dir", "", "Log directory")
	fs.BoolVar(&ea.verbose, "v", false, "Enable verbose output")
	fs.BoolVar(&ea.verbose, "verbose", false, "Enable verbose output")

	// Quality settings
	fs.StringVar(&ea.crf, "crf", "", "CRF quality level (single value or SD,HD,UHD)")
	fs.UintVar(&ea.preset, "preset", 0, "SVT-AV1 encoder preset (0-13)")

	// Processing options
	fs.BoolVar(&ea.disableAutocrop, "disable-autocrop", false, "Disable automatic crop detection")
	fs.IntVar(&ea.workers, "workers", defaultWorkers, "Number of parallel encoder workers")
	fs.IntVar(&ea.chunkBuffer, "buffer", defaultBuffer, "Extra chunks to buffer in memory")
	fs.IntVar(&ea.threads, "threads", config.DefaultThreadsPerWorker, "Threads per worker")

	// Output options
	fs.BoolVar(&ea.noLog, "no-log", false, "Disable log file creation")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate required arguments
	if ea.inputPath == "" {
		return fmt.Errorf("input path is required (-i/--input)")
	}
	if ea.outputDir == "" {
		return fmt.Errorf("output directory is required (-o/--output)")
	}

	return executeEncode(ea)
}

func executeEncode(ea encodeArgs) error {
	// Resolve input path
	inputPath, err := filepath.Abs(ea.inputPath)
	if err != nil {
		return fmt.Errorf("invalid input path: %w", err)
	}

	// Check if input exists
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("input path does not exist: %s", inputPath)
	}

	// Resolve output path
	outputDir, targetFilename, err := resolveOutputPath(inputPath, ea.outputDir, inputInfo.IsDir())
	if err != nil {
		return err
	}

	// Ensure output directory exists
	if err := util.EnsureDirectory(outputDir); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Resolve log directory
	logDir := ea.logDir
	if logDir == "" {
		logDir = logging.DefaultLogDir()
	}

	// Setup file logging
	logger, err := logging.Setup(logDir, ea.verbose, ea.noLog, os.Args)
	if err != nil {
		return fmt.Errorf("failed to setup logging: %w", err)
	}
	if logger != nil {
		defer func() { _ = logger.Close() }()
	}

	// Discover files to process
	var filesToProcess []string
	if inputInfo.IsDir() {
		filesToProcess, err = discovery.FindVideoFiles(inputPath)
		if err != nil {
			return fmt.Errorf("failed to discover video files: %w", err)
		}
		if len(filesToProcess) == 0 {
			return fmt.Errorf("no video files found in %s", inputPath)
		}
		if logger != nil {
			logger.Info("Discovered %d video files in %s", len(filesToProcess), inputPath)
			for i, f := range filesToProcess {
				logger.Debug("  %d. %s", i+1, f)
			}
		}
	} else {
		filesToProcess = []string{inputPath}
		if logger != nil {
			logger.Info("Processing single file: %s", inputPath)
		}
	}

	// Build configuration
	cfg := config.NewConfig(inputPath, outputDir, logDir)

	// Override with explicit CLI arguments
	if ea.crf != "" {
		if err := parseCRF(ea.crf, cfg); err != nil {
			return err
		}
	}
	if ea.preset != 0 {
		cfg.SVTAV1Preset = uint8(ea.preset)
	}
	if ea.disableAutocrop {
		cfg.CropMode = "none"
	}
	cfg.Workers = ea.workers
	cfg.ChunkBuffer = ea.chunkBuffer
	cfg.ThreadsPerWorker = ea.threads

	// Debug options
	cfg.Verbose = ea.verbose

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Log configuration
	if logger != nil {
		logger.Info("Output directory: %s", outputDir)
		logger.Info("CRF quality: SD=%d, HD=%d, UHD=%d", cfg.CRFSD, cfg.CRFHD, cfg.CRFUHD)
		logger.Info("SVT-AV1 preset: %d", cfg.SVTAV1Preset)
		logger.Info("Crop mode: %s", cfg.CropMode)
		logger.Info("Parallel encoding: workers=%d, buffer=%d, threads/worker=%d", cfg.Workers, cfg.ChunkBuffer, cfg.ThreadsPerWorker)
	}

	// Create reporters
	termRep := reporter.NewTerminalReporterVerbose(ea.verbose)
	var rep reporter.Reporter = termRep
	if logger != nil {
		// Combine terminal and log reporter so all events go to both
		logRep := reporter.NewLogReporter(logger.Writer())
		rep = reporter.NewCompositeReporter(termRep, logRep)
	}

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Run encoding
	_, err = processing.ProcessVideos(ctx, cfg, filesToProcess, targetFilename, rep)
	return err
}

// resolveOutputPath determines the output directory and optional target filename.
// If input is a file and output has a video extension, treat output as target filename.
func resolveOutputPath(_, outputPath string, isInputDir bool) (outputDir, targetFilename string, err error) {
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid output path: %w", err)
	}

	// If input is a directory, output must be a directory
	if isInputDir {
		return outputPath, "", nil
	}

	// Check if output path looks like a file (has video extension)
	ext := filepath.Ext(outputPath)
	videoExtensions := map[string]bool{
		".mkv": true, ".mp4": true, ".webm": true,
		".avi": true, ".mov": true, ".m4v": true,
	}

	if videoExtensions[ext] {
		// Output is a target filename
		return filepath.Dir(outputPath), filepath.Base(outputPath), nil
	}

	// Output is a directory
	return outputPath, "", nil
}

// parseCRF parses the CRF string and applies it to the config.
// Accepts either a single value (applied to all resolutions) or a comma-separated triple (SD,HD,UHD).
func parseCRF(crfStr string, cfg *config.Config) error {
	parts := strings.Split(crfStr, ",")

	switch len(parts) {
	case 1:
		// Single value: apply to all resolutions
		val, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 8)
		if err != nil {
			return fmt.Errorf("invalid CRF value %q: %w", crfStr, err)
		}
		cfg.CRFSD = uint8(val)
		cfg.CRFHD = uint8(val)
		cfg.CRFUHD = uint8(val)
	case 3:
		// Triple: SD,HD,UHD
		vals := make([]uint8, 3)
		for i, part := range parts {
			val, err := strconv.ParseUint(strings.TrimSpace(part), 10, 8)
			if err != nil {
				return fmt.Errorf("invalid CRF value in position %d: %w", i+1, err)
			}
			vals[i] = uint8(val)
		}
		cfg.CRFSD = vals[0]
		cfg.CRFHD = vals[1]
		cfg.CRFUHD = vals[2]
	default:
		return fmt.Errorf("--crf accepts single value or comma-separated triple (SD,HD,UHD), got %d values", len(parts))
	}

	return nil
}
