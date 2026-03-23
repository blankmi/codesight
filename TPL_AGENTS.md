# Code navigation policy

Use the lowest-cost operation that can answer the question.
Avoid reading full files unless necessary.

## Exact lookup
Use `grep`, `find`, or filename search when the target is specific:
- symbol names (class, function, type)
- exact strings, logs, constants
- filenames or paths
- import statements

Do NOT use `cs search` for exact matches.

## Conceptual discovery
Use `cs search "<question>" --path .` when:
- the relevant files are unknown
- the question is about behavior or architecture

Examples: how authentication works, where retries are implemented, how request context flows.

Do this BEFORE opening multiple files.

## Targeted reading
Use `cs extract -f <file> -s <symbol>` when:
- the file is large
- only one class, function, or type is needed

Only use `read_file` when:
- the file is small, or
- full context is clearly required

Before reading, identify all needed files first. Batch reads in parallel instead of reading one file at a time.

## Cross-file navigation
Use instead of repeated grep:
- `cs refs <symbol>` for references
- `cs callers <symbol>` for call hierarchy
- `cs implements <symbol>` for implementations

## Reading discipline
Do NOT:
- open many full files during exploration
- read large files before narrowing candidates
- read files sequentially when they can be batched

For conceptual tasks, follow this order strictly:
1. `cs search` to narrow scope
2. `cs extract` to inspect symbols
3. full file read only if needed

## Routing summary
- known target -> grep / find
- unknown area -> `cs search`
- single symbol -> `cs extract`
- relationships -> `cs refs` / `cs callers` / `cs implements`
