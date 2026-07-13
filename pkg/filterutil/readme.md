# filterutil

Shared utilities for filter resolution and path handling used by `sync-test-cases`.

## Purpose

This package provides common functionality for:
- Extracting stem names from file paths
- Finding project roots (.git or tests/sources.json)
- Loading and parsing refs.json files
- Resolving filter strings (stem or path) to destination directories

## Types

- `RefEntry` - Represents a source file entry in refs.json
- `RefPart` - Represents a destination part in refs.json

## Functions

- `FindProjectRoot()` - Searches for .git directory or tests/sources.json
- `ExtractStemFromFilter(filter)` - Extracts stem from a filter (stem or path)
- `LoadRefs(dirPath)` - Loads refs.json from a directory
- `FindDestinationForStem(baseDir, destinations, stem)` - Finds unique destination for a stem
- `ResolveFilterToDestination(baseDir, destinations, filter, originalFilter)` - Main resolver function

## Usage

`sync-test-cases` uses this package to handle filter parameters that can be either:
- Simple stem names: `murder-mystery-june-2025`
- Full paths: `tests/ipmt/files-md/murder-mystery-june-2025.ipmt`

The package handles stem extraction, path validation, and destination resolution automatically.
