# Repository navigation rules

Select tools based on query type. Do not default to broad operations.

## Exact lookup
Use `grep` or `find` for:
- known symbols and imports
- exact strings and constants
- file names and paths

Do not use `cs search` for exact matches.

## Conceptual discovery
Use `cs search "<question>" --path .` when:
- the implementation location is unknown
- the task is conceptual or architectural

Examples: how a feature works, where logic is implemented, how components interact.

If results are too broad, narrow the query instead of processing all results.

## Targeted reading
Use `cs extract -f <file> -s <symbol>` when:
- the file is large
- only one symbol is needed

Do not read full files or entire directories without narrowing candidates first.

## Cross-file navigation
Use instead of broad searches:
- `cs refs <symbol>` for references
- `cs callers <symbol>` for call hierarchy
- `cs implements <symbol>` for implementations

## Discipline
Do not:
- use `cs search` for simple lookups that `grep` can answer
- read multiple full files without narrowing first
- process large result sets without refining the query

## Routing summary
- known target -> grep / find
- unknown area -> `cs search`
- single symbol -> `cs extract`
- relationships -> `cs refs` / `cs callers` / `cs implements`
