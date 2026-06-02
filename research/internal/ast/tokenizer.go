package ast

import (
	"strings"
)

type TokenType int

const (
	TokIdent    TokenType = iota
	TokKeyword
	TokNumber
	TokString
	TokOperator
	TokOpenBrace
	TokCloseBrace
	TokOpenParen
	TokCloseParen
	TokOpenBracket
	TokCloseBracket
	TokSemicolon
	TokComma
	TokColon
	TokPreprocessor
	TokComment
	TokWhitespace
	TokEOF
)

type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

var cppKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "default": true, "break": true, "continue": true,
	"return": true, "goto": true, "struct": true, "class": true, "enum": true,
	"union": true, "namespace": true, "template": true, "typename": true,
	"using": true, "typedef": true, "static": true, "const": true, "volatile": true,
	"inline": true, "virtual": true, "override": true, "explicit": true,
	"public": true, "private": true, "protected": true, "friend": true,
	"new": true, "delete": true, "sizeof": true, "alignof": true,
	"try": true, "catch": true, "throw": true, "noexcept": true,
	"constexpr": true, "decltype": true, "auto": true, "nullptr": true,
	"int": true, "char": true, "short": true, "long": true, "float": true,
	"double": true, "bool": true, "void": true, "unsigned": true, "signed": true,
	"uint8_t": true, "uint16_t": true, "uint32_t": true, "uint64_t": true,
	"int8_t": true, "int16_t": true, "int32_t": true, "int64_t": true,
	"size_t": true, "true": true, "false": true,
	"VkResult": true, "tu_cs": true, "tu_bo": true, "tu_device": true,
}

func IsKeyword(s string) bool {
	return cppKeywords[s]
}

func Tokenize(source string) []Token {
	var tokens []Token
	line := 1
	col := 1
	i := 0
	runes := []rune(source)

	for i < len(runes) {
		ch := runes[i]

		switch {
		case ch == '\n':
			line++
			col = 1
			i++
			continue

		case ch == ' ' || ch == '\t' || ch == '\r':
			col++
			i++
			continue

		case ch == '/':
			if i+1 < len(runes) {
				if runes[i+1] == '/' {
					start := i
					for i < len(runes) && runes[i] != '\n' {
						i++
					}
					tokens = append(tokens, Token{TokComment, string(runes[start:i]), line, col})
					col += i - start
					continue
				}
				if runes[i+1] == '*' {
					start := i
					i += 2
					col += 2
					for i < len(runes)-1 && !(runes[i] == '*' && runes[i+1] == '/') {
						if runes[i] == '\n' {
							line++
							col = 1
						} else {
							col++
						}
						i++
					}
					if i < len(runes)-1 {
						i += 2
					}
					tokens = append(tokens, Token{TokComment, string(runes[start:i]), line, col})
					continue
				}
			}
			col++
			tokens = append(tokens, Token{TokOperator, string(ch), line, col - 1})
			i++

		case ch == '#':
			start := i
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			val := string(runes[start:i])
			if !strings.HasPrefix(val, "#include") {
				tokens = append(tokens, Token{TokPreprocessor, val, line, col})
			}
			col += i - start

		case ch == '"':
			start := i
			i++
			col++
			for i < len(runes) && runes[i] != '"' {
				if runes[i] == '\\' {
					i++
					col++
				}
				if runes[i] == '\n' {
					line++
					col = 1
				} else {
					col++
				}
				i++
			}
			if i < len(runes) {
				i++
				col++
			}
			tokens = append(tokens, Token{TokString, string(runes[start:i]), line, col})

		case ch == '\'':
			start := i
			i++
			col++
			for i < len(runes) && runes[i] != '\'' {
				if runes[i] == '\\' {
					i++
					col++
				}
				i++
				col++
			}
			if i < len(runes) {
				i++
				col++
			}
			tokens = append(tokens, Token{TokString, string(runes[start:i]), line, col})

		case ch == '{':
			tokens = append(tokens, Token{TokOpenBrace, "{", line, col})
			col++
			i++
		case ch == '}':
			tokens = append(tokens, Token{TokCloseBrace, "}", line, col})
			col++
			i++
		case ch == '(':
			tokens = append(tokens, Token{TokOpenParen, "(", line, col})
			col++
			i++
		case ch == ')':
			tokens = append(tokens, Token{TokCloseParen, ")", line, col})
			col++
			i++
		case ch == '[':
			tokens = append(tokens, Token{TokOpenBracket, "[", line, col})
			col++
			i++
		case ch == ']':
			tokens = append(tokens, Token{TokCloseBracket, "]", line, col})
			col++
			i++
		case ch == ';':
			tokens = append(tokens, Token{TokSemicolon, ";", line, col})
			col++
			i++
		case ch == ',':
			tokens = append(tokens, Token{TokComma, ",", line, col})
			col++
			i++
		case ch == ':':
			if i+1 < len(runes) && runes[i+1] == ':' {
				tokens = append(tokens, Token{TokColon, "::", line, col})
				i += 2
				col += 2
			} else {
				tokens = append(tokens, Token{TokColon, ":", line, col})
				col++
				i++
			}

		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_':
			start := i
			for i < len(runes) && (isIdent(runes[i])) {
				i++
				col++
			}
			val := string(runes[start:i])
			if IsKeyword(val) {
				tokens = append(tokens, Token{TokKeyword, val, line, col - (i - start)})
			} else {
				tokens = append(tokens, Token{TokIdent, val, line, col - (i - start)})
			}

		case ch >= '0' && ch <= '9':
			start := i
			for i < len(runes) && (isIdent(runes[i]) || runes[i] == '.') {
				i++
				col++
			}
			tokens = append(tokens, Token{TokNumber, string(runes[start:i]), line, col - (i - start)})

		default:
			if isOperatorChar(ch) {
				start := i
				for i < len(runes) && isOperatorChar(runes[i]) {
					i++
					col++
				}
				tokens = append(tokens, Token{TokOperator, string(runes[start:i]), line, col - (i - start)})
			} else {
				col++
				i++
			}
		}
	}

	tokens = append(tokens, Token{TokEOF, "", line, col})
	return tokens
}

func isIdent(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isOperatorChar(ch rune) bool {
	switch ch {
	case '+', '-', '*', '/', '%', '=', '!', '<', '>', '&', '|', '^', '~', '.', '?':
		return true
	}
	return false
}

type Parser struct {
	tokens   []Token
	pos      int
	errors   []string
	Comments []string
}

func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens, pos: 0}
}

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{TokEOF, "", 0, 0}
	}
	return p.tokens[p.pos]
}

func (p *Parser) next() Token {
	tok := p.peek()
	if tok.Type != TokEOF {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(t TokenType) (Token, error) {
	tok := p.next()
	if tok.Type != t {
		return tok, nil
	}
	return tok, nil
}

type ASTNode struct {
	Kind       string                 `json:"kind"`
	Name       string                 `json:"name"`
	Signature  string                 `json:"signature,omitempty"`
	Line       int                    `json:"line"`
	File       string                 `json:"file,omitempty"`
	Children   []*ASTNode             `json:"children,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Gen8       bool                   `json:"gen8"`
}

func (p *Parser) ParseFunction() *ASTNode {
	tok := p.peek()
	if tok.Type != TokIdent {
		return nil
	}

	name := tok.Value
	sigStart := p.pos
	sigEnd := p.pos

	fn := &ASTNode{
		Kind:       "function",
		Name:       name,
		Line:       tok.Line,
		Properties: make(map[string]interface{}),
	}

	for p.peek().Type != TokOpenBrace && p.peek().Type != TokSemicolon && p.peek().Type != TokEOF {
		t := p.next()
		sigEnd = p.pos
		if t.Value == "const" || t.Value == "override" || t.Value == "noexcept" {
			fn.Properties[t.Value] = true
		}
	}

	if p.peek().Type == TokSemicolon {
		p.next()
		return nil
	}

	if p.peek().Type != TokOpenBrace {
		return fn
	}

	p.next()

	braceDepth := 1
	var body []Token
	for braceDepth > 0 && p.pos < len(p.tokens) {
		tok := p.next()
		switch tok.Type {
		case TokOpenBrace:
			braceDepth++
		case TokCloseBrace:
			braceDepth--
			if braceDepth == 0 {
				break
			}
		}
		if braceDepth > 0 {
			body = append(body, tok)
		}
	}

	fn.Children = p.parseBlock(body)

	if sigEnd > sigStart {
		var sigParts []string
		for i := sigStart; i < sigEnd && i < len(p.tokens); i++ {
			t := p.tokens[i]
			if t.Type != TokComment && t.Type != TokWhitespace && t.Type != TokPreprocessor {
				sigParts = append(sigParts, t.Value)
			}
		}
		fn.Signature = strings.Join(sigParts, " ")
	}

	return fn
}

func (p *Parser) parseBlock(tokens []Token) []*ASTNode {
	var nodes []*ASTNode
	i := 0
	for i < len(tokens) {
		tok := tokens[i]

		switch {
		case tok.Type == TokKeyword && tok.Value == "if":
			node, consumed := p.parseIfStmt(tokens, i)
			if node != nil {
				nodes = append(nodes, node)
			}
			i += consumed
			continue

		case tok.Type == TokKeyword && tok.Value == "for":
			for i < len(tokens) && tokens[i].Type != TokOpenBrace {
				i++
			}
			if i < len(tokens) {
				i++
				depth := 1
				for depth > 0 && i < len(tokens) {
					switch tokens[i].Type {
					case TokOpenBrace:
						depth++
					case TokCloseBrace:
						depth--
					}
					i++
				}
			}
			continue

		case tok.Type == TokKeyword && tok.Value == "switch":
			for i < len(tokens) && tokens[i].Type != TokOpenBrace {
				i++
			}
			if i < len(tokens) {
				i++
				depth := 1
				for depth > 0 && i < len(tokens) {
					switch tokens[i].Type {
					case TokOpenBrace:
						depth++
					case TokCloseBrace:
						depth--
					}
					i++
				}
			}
			continue

		case tok.Value == "A8XX" || tok.Value == "CHIP":
			node := p.parseGen8Pattern(tokens, &i)
			if node != nil {
				nodes = append(nodes, node)
			}
			continue

		case strings.HasPrefix(tok.Value, "tu_cs_emit") || strings.HasPrefix(tok.Value, "tu_cs_") ||
			strings.HasPrefix(tok.Value, "tu6_emit_") || strings.HasPrefix(tok.Value, "tu_bo_") ||
			strings.HasPrefix(tok.Value, "tu_device") || strings.HasPrefix(tok.Value, "tu_pipeline") ||
			strings.HasPrefix(tok.Value, "tu_submit") || strings.HasPrefix(tok.Value, "kgsl_") ||
			strings.HasPrefix(tok.Value, "tu_clear") || strings.HasPrefix(tok.Value, "tu_blit") ||
			strings.HasPrefix(tok.Value, "tu_image") || strings.HasPrefix(tok.Value, "tu_sampler") ||
			strings.HasPrefix(tok.Value, "tu_lrz") || strings.HasPrefix(tok.Value, "tu_tess") ||
			strings.HasPrefix(tok.Value, "pkt_field_set") || strings.HasPrefix(tok.Value, "pkt_field_get"):
			node := p.parseFuncCall(tokens, &i)
			if node != nil {
				nodes = append(nodes, node)
			}
			continue

		case tok.Value == "vk_errorf" || tok.Value == "vk_error" || tok.Value == "vk_out_of_host_memory" ||
			tok.Value == "VK_ERROR":
			node := p.parseErrorCall(tokens, &i)
			if node != nil {
				nodes = append(nodes, node)
			}
			continue

		case tok.Type == TokKeyword && tok.Value == "struct":
			node := p.parseStructDef(tokens, &i)
			if node != nil {
				nodes = append(nodes, node)
			}
			continue
		}

		i++
	}
	return nodes
}

func (p *Parser) parseIfStmt(tokens []Token, start int) (*ASTNode, int) {
	if start >= len(tokens) {
		return nil, 0
	}

	consumed := 1
	condition := ""
	for start+consumed < len(tokens) && tokens[start+consumed].Type != TokOpenBrace && tokens[start+consumed].Type != TokSemicolon {
		t := tokens[start+consumed]
		if t.Type != TokComment && t.Type != TokWhitespace {
			if condition != "" {
				condition += " "
			}
			condition += t.Value
		}
		consumed++
	}
	if start+consumed >= len(tokens) {
		return nil, consumed
	}

	isGen8 := strings.Contains(condition, "A8XX") && (strings.Contains(condition, "CHIP") || strings.Contains(condition, ">=") || strings.Contains(condition, "<"))

	node := &ASTNode{
		Kind:       "if_statement",
		Name:       condition,
		Line:       tokens[start].Line,
		Gen8:       isGen8,
		Properties: map[string]interface{}{"condition": condition},
	}

	if tokens[start+consumed].Type == TokSemicolon {
		consumed++
		return node, consumed
	}

	consumed++
	depth := 1
	var body []Token
	for depth > 0 && start+consumed < len(tokens) {
		t := tokens[start+consumed]
		switch t.Type {
		case TokOpenBrace:
			depth++
		case TokCloseBrace:
			depth--
			if depth == 0 {
				consumed++
				break
			}
		}
		if depth > 0 {
			body = append(body, t)
		}
		consumed++
	}

	node.Children = p.parseBlock(body)

	if start+consumed < len(tokens) && tokens[start+consumed].Type == TokKeyword && tokens[start+consumed].Value == "else" {
		consumed++
		if start+consumed < len(tokens) {
			if tokens[start+consumed].Type == TokKeyword && tokens[start+consumed].Value == "if" {
				elseNode, elseConsumed := p.parseIfStmt(tokens, start+consumed-1)
				consumed += elseConsumed - 1
				if elseNode != nil {
					node.Children = append(node.Children, elseNode)
				}
			} else if tokens[start+consumed].Type == TokOpenBrace {
				consumed++
				depth := 1
				var body []Token
				for depth > 0 && start+consumed < len(tokens) {
					t := tokens[start+consumed]
					switch t.Type {
					case TokOpenBrace:
						depth++
					case TokCloseBrace:
						depth--
						if depth == 0 {
							consumed++
							break
						}
					}
					if depth > 0 {
						body = append(body, t)
					}
					consumed++
				}
				elseBlk := &ASTNode{
					Kind: "else_block",
					Line: tokens[start+consumed-len(body)-1].Line,
					Gen8: isGen8,
				}
				elseBlk.Children = p.parseBlock(body)
				if len(body) > 0 {
					elseBlk.Properties = map[string]interface{}{
						"body_length": len(body),
					}
				}
				node.Children = append(node.Children, elseBlk)
			}
		}
	}

	return node, consumed
}

func (p *Parser) parseFuncCall(tokens []Token, pos *int) *ASTNode {
	if *pos >= len(tokens) {
		return nil
	}

	name := tokens[*pos].Value
	line := tokens[*pos].Line

	node := &ASTNode{
		Kind: "function_call",
		Name: name,
		Line: line,
	}

	*pos++
	if *pos < len(tokens) && tokens[*pos].Type == TokOperator && tokens[*pos].Value == "." {
		*pos++
		if *pos < len(tokens) && tokens[*pos].Type == TokIdent {
			node.Name = name + "." + tokens[*pos].Value
			*pos++
		}
	}

	if *pos < len(tokens) && tokens[*pos].Type == TokOperator && tokens[*pos].Value == "<" {
		depth := 1
		*pos++
		var tmplArgs []string
		for depth > 0 && *pos < len(tokens) {
			if tokens[*pos].Type == TokOperator && tokens[*pos].Value == "<" {
				depth++
			} else if tokens[*pos].Type == TokOperator && tokens[*pos].Value == ">" {
				depth--
				if depth == 0 {
					*pos++
					break
				}
			}
			if depth > 0 && tokens[*pos].Type == TokIdent {
				tmplArgs = append(tmplArgs, tokens[*pos].Value)
			}
			*pos++
		}
		if len(tmplArgs) > 0 {
			node.Name = name + "<" + strings.Join(tmplArgs, ",") + ">"
		}
	}

	if *pos < len(tokens) && tokens[*pos].Type == TokOpenParen {
		depth := 1
		*pos++
		for depth > 0 && *pos < len(tokens) {
			switch tokens[*pos].Type {
			case TokOpenParen:
				depth++
			case TokCloseParen:
				depth--
			}
			*pos++
		}
	}

	if strings.Contains(name, "A8XX") || strings.Contains(name, "gen8") || strings.Contains(name, "a8xx") {
		node.Gen8 = true
	}

	return node
}

func (p *Parser) parseErrorCall(tokens []Token, pos *int) *ASTNode {
	if *pos >= len(tokens) {
		return nil
	}

	node := &ASTNode{
		Kind: "error_call",
		Name: tokens[*pos].Value,
		Line: tokens[*pos].Line,
	}

	*pos++
	if *pos < len(tokens) && tokens[*pos].Type == TokOpenParen {
		depth := 1
		*pos++
		for depth > 0 && *pos < len(tokens) {
			switch tokens[*pos].Type {
			case TokOpenParen:
				depth++
			case TokCloseParen:
				depth--
			}
			*pos++
		}
	}

	return node
}

func (p *Parser) parseStructDef(tokens []Token, pos *int) *ASTNode {
	if *pos >= len(tokens) {
		return nil
	}

	name := ""
	for *pos+1 < len(tokens) && tokens[*pos+1].Type == TokIdent {
		*pos++
		name = tokens[*pos].Value
	}
	if name == "" {
		return nil
	}

	node := &ASTNode{
		Kind: "struct",
		Name: name,
		Line: tokens[*pos].Line,
	}

	if *pos+1 < len(tokens) {
		if tokens[*pos+1].Type == TokSemicolon {
			*pos++
			return node
		}
		if tokens[*pos+1].Type == TokColon && tokens[*pos+1].Value == ":" {
			*pos++
			for *pos < len(tokens) && tokens[*pos].Type != TokOpenBrace && tokens[*pos].Type != TokSemicolon {
				*pos++
			}
		}
	}

	if *pos+1 < len(tokens) && tokens[*pos+1].Type == TokOpenBrace {
		*pos++
		*pos++
		depth := 1
		var structBody []Token
		for depth > 0 && *pos < len(tokens) {
			switch tokens[*pos].Type {
			case TokOpenBrace:
				depth++
			case TokCloseBrace:
				depth--
				if depth == 0 {
					*pos++
					break
				}
			}
			if depth > 0 {
				structBody = append(structBody, tokens[*pos])
			}
			*pos++
		}
		node.Children = p.parseStructFields(structBody)
	}

	return node
}

func (p *Parser) parseStructFields(tokens []Token) []*ASTNode {
	var fields []*ASTNode
	i := 0
	for i < len(tokens) {
		if tokens[i].Type == TokIdent && !IsKeyword(tokens[i].Value) {
			name := tokens[i].Value

			if i+1 >= len(tokens) || tokens[i+1].Type != TokSemicolon {
				if i+1 < len(tokens) && (tokens[i+1].Type == TokOperator) {
					i++
					for i < len(tokens) && tokens[i].Type != TokSemicolon {
						i++
					}
					if i < len(tokens) {
						i++
					}
					continue
				}

				if startsWith(name, "A8XX") || strings.Contains(name, "gen8") {
					field := &ASTNode{
						Kind: "struct_field",
						Name: name,
						Line: tokens[i].Line,
						Gen8: true,
					}
					fields = append(fields, field)
				} else if strings.Contains(name, "tu_") || strings.Contains(name, "kgsl_") {
					field := &ASTNode{
						Kind: "struct_field",
						Name: name,
						Line: tokens[i].Line,
					}
					fields = append(fields, field)
				}
			}

			for i < len(tokens) && tokens[i].Type != TokSemicolon {
				i++
			}
			if i < len(tokens) {
				i++
			}
			continue
		}
		i++
	}
	return fields
}

func (p *Parser) parseGen8Pattern(tokens []Token, pos *int) *ASTNode {
	if *pos >= len(tokens) {
		return nil
	}

	node := &ASTNode{
		Kind: "gen8_pattern",
		Name: tokens[*pos].Value,
		Line: tokens[*pos].Line,
		Gen8: true,
	}

	start := *pos
	for *pos < len(tokens) && *pos-start < 20 && tokens[*pos].Type != TokSemicolon && tokens[*pos].Type != TokOpenBrace {
		if tokens[*pos].Value == "A8XX" || strings.Contains(tokens[*pos].Value, "gen8") {
			node.Gen8 = true
		}
		*pos++
	}

	return node
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
