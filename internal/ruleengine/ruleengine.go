package ruleengine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Context holds runtime values for evaluating custom listing deal equations.
type Context struct {
	Price          float64
	Score          float64
	TargetMinPrice float64
	TargetMaxPrice float64
	MaxAlertPrice  float64
	MinAlertPrice  float64
	AlertThreshold float64
	HasPhoto       bool
	Source         string
}

// Preset definitions for common deal scenarios.
const (
	PresetGreatDeal      = "great_deal"
	PresetStealDeal      = "steal_deal"
	PresetHighMatchPhoto = "high_match_photo"
	PresetBudgetLimit    = "budget_limit"
	PresetAnyMatch       = "any_match"
	PresetCustom         = "custom"
)

// PresetDescriptions provides bilingual human-readable descriptions of presets.
var PresetDescriptions = map[string]map[string]string{
	PresetGreatDeal: {
		"en": "Great Deal (price within target and score meets alert threshold)",
		"lv": "Laba cena (cena ir mērķa robežās un atbilstība sasniedz slieksni)",
	},
	PresetStealDeal: {
		"en": "Steal Deal (price ≥ 25% below target price with good match)",
		"lv": "Īpaši izdevīgs (cena ir vismaz 25% zem mērķa cenas)",
	},
	PresetHighMatchPhoto: {
		"en": "High Match + Photo (score ≥ 80%, photo required, price within target)",
		"lv": "Augsta atbilstība ar foto (atbilstība ≥ 80%, foto obligāts, cena mērķī)",
	},
	PresetBudgetLimit: {
		"en": "Strict Budget (price ≤ target max price with medium/high score)",
		"lv": "Stingrs budžets (cena ≤ maksimālā mērķa cena)",
	},
	PresetAnyMatch: {
		"en": "Match Only (alert on any match meeting threshold, ignore price)",
		"lv": "Jebkura atbilstība (ziņot par jebkuru atbilstību, neievērojot cenu)",
	},
	PresetCustom: {
		"en": "Custom Equation (custom user-defined formula)",
		"lv": "Pielāgots vienādojums (lietotāja veidota formula)",
	},
}

// ResolvePresetEquation returns the concrete equation for a given preset.
func ResolvePresetEquation(preset string, targetMaxPrice, alertThreshold float64) string {
	switch preset {
	case PresetGreatDeal:
		if targetMaxPrice > 0 {
			return fmt.Sprintf("price > 0 && price <= target_max_price && score >= alert_threshold")
		}
		return "score >= alert_threshold"
	case PresetStealDeal:
		if targetMaxPrice > 0 {
			return fmt.Sprintf("price > 0 && price <= (target_max_price * 0.75) && score >= (alert_threshold - 10)")
		}
		return "score >= (alert_threshold - 10)"
	case PresetHighMatchPhoto:
		if targetMaxPrice > 0 {
			return "score >= 80 && has_photo == 1 && price <= target_max_price"
		}
		return "score >= 80 && has_photo == 1"
	case PresetBudgetLimit:
		if targetMaxPrice > 0 {
			return "price > 0 && price <= target_max_price && score >= 60"
		}
		return "score >= 60"
	case PresetAnyMatch:
		return "score >= alert_threshold"
	default:
		if targetMaxPrice > 0 {
			return "price > 0 && price <= target_max_price && score >= alert_threshold"
		}
		return "score >= alert_threshold"
	}
}

// Validate parses an expression with dummy context to verify syntax and variables.
func Validate(expr string) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	dummyCtx := Context{
		Price:          100.0,
		Score:          80.0,
		TargetMinPrice: 50.0,
		TargetMaxPrice: 150.0,
		MaxAlertPrice:  200.0,
		MinAlertPrice:  0.0,
		AlertThreshold: 70.0,
		HasPhoto:       true,
		Source:         "test",
	}
	p := newParser(expr, dummyCtx)
	_, err := p.parseExpression()
	if err != nil {
		return false, fmt.Errorf("syntax error in equation '%s': %w", expr, err)
	}
	return true, nil
}

// Evaluate parses and executes an expression against the given context.
// Returns whether the condition passes, a descriptive explanation, and an error if parsing failed.
func Evaluate(expr string, ctx Context) (bool, string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		// Default rule: score >= alert_threshold, and if max_alert_price > 0, price <= max_alert_price
		if ctx.Score < ctx.AlertThreshold {
			return false, "", nil
		}
		if ctx.MaxAlertPrice > 0 && ctx.Price > ctx.MaxAlertPrice {
			return false, "", nil
		}
		if ctx.MinAlertPrice > 0 && ctx.Price < ctx.MinAlertPrice {
			return false, "", nil
		}
		return true, formatDefaultExplanation(ctx), nil
	}

	p := newParser(expr, ctx)
	val, err := p.parseExpression()
	if err != nil {
		return false, "", fmt.Errorf("evaluating equation '%s': %w", expr, err)
	}

	passed := val.toBool()
	if !passed {
		return false, "", nil
	}

	reason := formatExplanation(expr, ctx)
	return true, reason, nil
}

func formatDefaultExplanation(ctx Context) string {
	if ctx.Price > 0 {
		return fmt.Sprintf("Match score %d%% meets threshold %d%% (Price: €%.2f)",
			int(ctx.Score), int(ctx.AlertThreshold), ctx.Price)
	}
	return fmt.Sprintf("Match score %d%% meets threshold %d%%",
		int(ctx.Score), int(ctx.AlertThreshold))
}

func formatExplanation(expr string, ctx Context) string {
	var parts []string
	if ctx.Price > 0 {
		parts = append(parts, fmt.Sprintf("€%.2f", ctx.Price))
	}
	parts = append(parts, fmt.Sprintf("score %d%%", int(ctx.Score)))
	if ctx.HasPhoto {
		parts = append(parts, "photo verified")
	}
	return fmt.Sprintf("Alert rule met: %s (%s)", expr, strings.Join(parts, ", "))
}

// -------------------------------------------------------------
// Lexer & Recursive Descent Parser
// -------------------------------------------------------------

type tokenType int

const (
	tokEOF tokenType = iota
	tokNumber
	tokString
	tokIdent
	tokLParen
	tokRParen
	tokOp
)

type token struct {
	typ tokenType
	str string
	num float64
}

type parser struct {
	src    string
	pos    int
	ctx    Context
	tokens []token
	curr   int
}

func newParser(src string, ctx Context) *parser {
	p := &parser{src: src, ctx: ctx}
	p.tokenize()
	return p
}

func (p *parser) tokenize() {
	s := p.src
	i := 0
	n := len(s)

	for i < n {
		c := rune(s[i])
		if unicode.IsSpace(c) {
			i++
			continue
		}

		if c == '(' {
			p.tokens = append(p.tokens, token{typ: tokLParen, str: "("})
			i++
			continue
		}
		if c == ')' {
			p.tokens = append(p.tokens, token{typ: tokRParen, str: ")"})
			i++
			continue
		}

		// Two-character operators
		if i+1 < n {
			two := s[i : i+2]
			if two == "==" || two == "!=" || two == "<=" || two == ">=" || two == "&&" || two == "||" {
				p.tokens = append(p.tokens, token{typ: tokOp, str: two})
				i += 2
				continue
			}
		}

		// Single-character operators
		if strings.ContainsRune("+-*/<>!", c) {
			p.tokens = append(p.tokens, token{typ: tokOp, str: string(c)})
			i++
			continue
		}

		// String literal 'ss.com' or "ss.com"
		if c == '\'' || c == '"' {
			quote := c
			i++
			start := i
			for i < n && rune(s[i]) != quote {
				i++
			}
			lit := s[start:i]
			if i < n {
				i++ // skip closing quote
			}
			p.tokens = append(p.tokens, token{typ: tokString, str: lit})
			continue
		}

		// Number literal
		if unicode.IsDigit(c) || (c == '.' && i+1 < n && unicode.IsDigit(rune(s[i+1]))) {
			start := i
			for i < n && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
				i++
			}
			raw := s[start:i]
			num, _ := strconv.ParseFloat(raw, 64)
			p.tokens = append(p.tokens, token{typ: tokNumber, str: raw, num: num})
			continue
		}

		// Identifiers (variable names, and/or keywords)
		if unicode.IsLetter(c) || c == '_' {
			start := i
			for i < n && (unicode.IsLetter(rune(s[i])) || unicode.IsDigit(rune(s[i])) || s[i] == '_') {
				i++
			}
			raw := s[start:i]
			lower := strings.ToLower(raw)
			if lower == "and" {
				p.tokens = append(p.tokens, token{typ: tokOp, str: "&&"})
			} else if lower == "or" {
				p.tokens = append(p.tokens, token{typ: tokOp, str: "||"})
			} else if lower == "not" {
				p.tokens = append(p.tokens, token{typ: tokOp, str: "!"})
			} else {
				p.tokens = append(p.tokens, token{typ: tokIdent, str: raw})
			}
			continue
		}

		// Unknown symbol, skip
		i++
	}

	p.tokens = append(p.tokens, token{typ: tokEOF})
}

func (p *parser) peek() token {
	if p.curr >= len(p.tokens) {
		return token{typ: tokEOF}
	}
	return p.tokens[p.curr]
}

func (p *parser) next() token {
	tok := p.peek()
	if tok.typ != tokEOF {
		p.curr++
	}
	return tok
}

// evalValue holds a numeric, boolean, or string value.
type evalValue struct {
	isNum bool
	num   float64
	isStr bool
	str   string
	isB   bool
	b     bool
}

func (v evalValue) toBool() bool {
	if v.isB {
		return v.b
	}
	if v.isNum {
		return v.num != 0
	}
	if v.isStr {
		return len(v.str) > 0
	}
	return false
}

func (v evalValue) toFloat() float64 {
	if v.isNum {
		return v.num
	}
	if v.isB {
		if v.b {
			return 1
		}
		return 0
	}
	if v.isStr {
		f, _ := strconv.ParseFloat(v.str, 64)
		return f
	}
	return 0
}

func (p *parser) parseExpression() (evalValue, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (evalValue, error) {
	left, err := p.parseAnd()
	if err != nil {
		return evalValue{}, err
	}

	for p.peek().str == "||" {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return evalValue{}, err
		}
		left = evalValue{isB: true, b: left.toBool() || right.toBool()}
	}

	return left, nil
}

func (p *parser) parseAnd() (evalValue, error) {
	left, err := p.parseRelational()
	if err != nil {
		return evalValue{}, err
	}

	for p.peek().str == "&&" {
		p.next()
		right, err := p.parseRelational()
		if err != nil {
			return evalValue{}, err
		}
		left = evalValue{isB: true, b: left.toBool() && right.toBool()}
	}

	return left, nil
}

func (p *parser) parseRelational() (evalValue, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return evalValue{}, err
	}

	for {
		op := p.peek().str
		if op != "==" && op != "!=" && op != "<" && op != "<=" && op != ">" && op != ">=" {
			break
		}
		p.next()
		right, err := p.parseAdditive()
		if err != nil {
			return evalValue{}, err
		}

		var res bool
		if left.isStr || right.isStr {
			s1 := left.str
			s2 := right.str
			switch op {
			case "==":
				res = strings.EqualFold(s1, s2)
			case "!=":
				res = !strings.EqualFold(s1, s2)
			default:
				res = false
			}
		} else {
			n1 := left.toFloat()
			n2 := right.toFloat()
			switch op {
			case "==":
				res = math.Abs(n1-n2) < 0.00001
			case "!=":
				res = math.Abs(n1-n2) >= 0.00001
			case "<":
				res = n1 < n2
			case "<=":
				res = n1 <= n2
			case ">":
				res = n1 > n2
			case ">=":
				res = n1 >= n2
			}
		}

		left = evalValue{isB: true, b: res}
	}

	return left, nil
}

func (p *parser) parseAdditive() (evalValue, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return evalValue{}, err
	}

	for {
		op := p.peek().str
		if op != "+" && op != "-" {
			break
		}
		p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return evalValue{}, err
		}

		if op == "+" {
			left = evalValue{isNum: true, num: left.toFloat() + right.toFloat()}
		} else {
			left = evalValue{isNum: true, num: left.toFloat() - right.toFloat()}
		}
	}

	return left, nil
}

func (p *parser) parseMultiplicative() (evalValue, error) {
	left, err := p.parseUnary()
	if err != nil {
		return evalValue{}, err
	}

	for {
		op := p.peek().str
		if op != "*" && op != "/" {
			break
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return evalValue{}, err
		}

		if op == "*" {
			left = evalValue{isNum: true, num: left.toFloat() * right.toFloat()}
		} else {
			denom := right.toFloat()
			if denom == 0 {
				denom = 1
			}
			left = evalValue{isNum: true, num: left.toFloat() / denom}
		}
	}

	return left, nil
}

func (p *parser) parseUnary() (evalValue, error) {
	if p.peek().str == "!" {
		p.next()
		val, err := p.parseUnary()
		if err != nil {
			return evalValue{}, err
		}
		return evalValue{isB: true, b: !val.toBool()}, nil
	}

	if p.peek().str == "-" {
		p.next()
		val, err := p.parseUnary()
		if err != nil {
			return evalValue{}, err
		}
		return evalValue{isNum: true, num: -val.toFloat()}, nil
	}

	return p.parsePrimary()
}

func (p *parser) parsePrimary() (evalValue, error) {
	tok := p.next()

	switch tok.typ {
	case tokNumber:
		return evalValue{isNum: true, num: tok.num}, nil
	case tokString:
		return evalValue{isStr: true, str: tok.str}, nil
	case tokIdent:
		val := p.resolveVariable(tok.str)
		return val, nil
	case tokLParen:
		val, err := p.parseExpression()
		if err != nil {
			return evalValue{}, err
		}
		if p.peek().typ != tokRParen {
			return evalValue{}, fmt.Errorf("expected ')' but got '%s'", p.peek().str)
		}
		p.next()
		return val, nil
	default:
		return evalValue{}, fmt.Errorf("unexpected token '%s'", tok.str)
	}
}

func (p *parser) resolveVariable(name string) evalValue {
	name = strings.ToLower(name)
	switch name {
	case "price":
		return evalValue{isNum: true, num: p.ctx.Price}
	case "score":
		return evalValue{isNum: true, num: p.ctx.Score}
	case "target_min_price", "min_price":
		return evalValue{isNum: true, num: p.ctx.TargetMinPrice}
	case "target_max_price", "max_price":
		return evalValue{isNum: true, num: p.ctx.TargetMaxPrice}
	case "max_alert_price":
		return evalValue{isNum: true, num: p.ctx.MaxAlertPrice}
	case "min_alert_price":
		return evalValue{isNum: true, num: p.ctx.MinAlertPrice}
	case "alert_threshold":
		return evalValue{isNum: true, num: p.ctx.AlertThreshold}
	case "has_photo":
		if p.ctx.HasPhoto {
			return evalValue{isNum: true, num: 1, isB: true, b: true}
		}
		return evalValue{isNum: true, num: 0, isB: true, b: false}
	case "source":
		return evalValue{isStr: true, str: strings.ToLower(p.ctx.Source)}
	case "true":
		return evalValue{isB: true, b: true, isNum: true, num: 1}
	case "false":
		return evalValue{isB: true, b: false, isNum: true, num: 0}
	default:
		return evalValue{isNum: true, num: 0}
	}
}
