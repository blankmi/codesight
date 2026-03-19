package lsp

import "encoding/json"

const (
	// JSONRPCVersion is the protocol version used by LSP messages.
	JSONRPCVersion = "2.0"

	MethodInitialize                       = "initialize"
	MethodInitialized                      = "initialized"
	MethodShutdown                         = "shutdown"
	MethodExit                             = "exit"
	MethodWorkspaceSymbol                  = "workspace/symbol"
	MethodTextDocumentReferences           = "textDocument/references"
	MethodTextDocumentImplementation       = "textDocument/implementation"
	MethodTextDocumentPrepareCallHierarchy = "textDocument/prepareCallHierarchy"
	MethodCallHierarchyIncomingCalls       = "callHierarchy/incomingCalls"
)

// RequestMessage represents a JSON-RPC request message.
type RequestMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// NotificationMessage represents a JSON-RPC notification message.
type NotificationMessage struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// ResponseMessage represents a JSON-RPC response message.
type ResponseMessage struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id"`
	Result  json.RawMessage    `json:"result,omitempty"`
	Error   *ResponseErrorBody `json:"error,omitempty"`
}

// ResponseErrorBody is the JSON-RPC error payload returned by the server.
type ResponseErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// DocumentURI is an LSP URI reference to a text document.
type DocumentURI string

// Position identifies a UTF-16 code-unit position in a text document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range identifies a span in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location identifies one concrete location in a source file.
type Location struct {
	URI   DocumentURI `json:"uri"`
	Range Range       `json:"range"`
}

// TextDocumentIdentifier points to a text document by URI.
type TextDocumentIdentifier struct {
	URI DocumentURI `json:"uri"`
}

// TextDocumentPositionParams combines a document identifier and a position.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// WorkspaceSymbolParams describes a workspace/symbol request.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// SymbolKind is the LSP symbol kind enum.
type SymbolKind int

const (
	SymbolKindFile SymbolKind = 1 + iota
	SymbolKindModule
	SymbolKindNamespace
	SymbolKindPackage
	SymbolKindClass
	SymbolKindMethod
	SymbolKindProperty
	SymbolKindField
	SymbolKindConstructor
	SymbolKindEnum
	SymbolKindInterface
	SymbolKindFunction
	SymbolKindVariable
	SymbolKindConstant
	SymbolKindString
	SymbolKindNumber
	SymbolKindBoolean
	SymbolKindArray
	SymbolKindObject
	SymbolKindKey
	SymbolKindNull
	SymbolKindEnumMember
	SymbolKindStruct
	SymbolKindEvent
	SymbolKindOperator
	SymbolKindTypeParameter
)

// SymbolInformation describes one symbol match from workspace/symbol.
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// ReferenceContext controls behavior for reference lookups.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams describes textDocument/references params.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// CallHierarchyPrepareParams describes call hierarchy preparation params.
type CallHierarchyPrepareParams struct {
	TextDocumentPositionParams
}

// CallHierarchyItem identifies a callable symbol for call hierarchy APIs.
type CallHierarchyItem struct {
	Name           string      `json:"name"`
	Kind           SymbolKind  `json:"kind"`
	URI            DocumentURI `json:"uri"`
	Range          Range       `json:"range"`
	SelectionRange Range       `json:"selectionRange"`
	Detail         string      `json:"detail,omitempty"`
}

// CallHierarchyIncomingCallsParams requests incoming callers for an item.
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyIncomingCall describes one incoming caller edge.
type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// ClientInfo identifies this client instance to the language server.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// WorkspaceFolder represents an LSP workspace folder.
type WorkspaceFolder struct {
	URI  DocumentURI `json:"uri"`
	Name string      `json:"name"`
}

// InitializeParams captures the initialize request payload.
type InitializeParams struct {
	ProcessID             *int              `json:"processId,omitempty"`
	RootURI               DocumentURI       `json:"rootUri,omitempty"`
	ClientInfo            *ClientInfo       `json:"clientInfo,omitempty"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
	Capabilities          map[string]any    `json:"capabilities"`
	WorkspaceFolders      []WorkspaceFolder `json:"workspaceFolders,omitempty"`
}

// InitializedParams captures the initialized notification payload.
type InitializedParams struct{}

// ServerInfo identifies the language server implementation.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult captures the initialize response payload.
type InitializeResult struct {
	Capabilities map[string]any `json:"capabilities"`
	ServerInfo   *ServerInfo    `json:"serverInfo,omitempty"`
}
