package cli

import (
	"fmt"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type invocation struct {
	Definition  CommandDefinition
	JSON        bool
	Positionals []string
	Values      map[string][]string
	Flags       map[string]bool
}

func parseInvocation(args []string) (invocation, error) {
	filtered := make([]string, 0, len(args))
	asJSON := false
	for _, argument := range args {
		if argument == "--json" {
			asJSON = true
			continue
		}
		filtered = append(filtered, argument)
	}
	if len(filtered) == 0 {
		filtered = []string{"help"}
	}
	definition, words, ok := matchDefinition(filtered)
	if !ok {
		return invocation{JSON: asJSON}, usageError("unknown command")
	}
	result := invocation{
		Definition: definition, JSON: asJSON,
		Values: map[string][]string{}, Flags: map[string]bool{},
	}
	options := make(map[string]Option, len(definition.Options))
	for _, option := range definition.Options {
		options[option.Name] = option
	}
	for len(words) > 0 {
		argument := words[0]
		if !strings.HasPrefix(argument, "--") {
			result.Positionals = append(result.Positionals, argument)
			words = words[1:]
			continue
		}
		nameValue := strings.TrimPrefix(argument, "--")
		name, inlineValue, hasInline := strings.Cut(nameValue, "=")
		option, exists := options[name]
		if !exists {
			return result, usageError("unknown option --" + name + " for " + definition.Path)
		}
		if option.Boolean {
			if hasInline {
				return result, usageError("boolean option --" + name + " does not take a value")
			}
			result.Flags[name] = true
			words = words[1:]
			continue
		}
		var value string
		if hasInline {
			value = inlineValue
			words = words[1:]
		} else {
			if len(words) < 2 || strings.HasPrefix(words[1], "--") {
				return result, usageError("--" + name + " requires a value")
			}
			value = words[1]
			words = words[2:]
		}
		if !option.Repeatable && len(result.Values[name]) > 0 {
			return result, usageError("duplicate option --" + name)
		}
		result.Values[name] = append(result.Values[name], value)
	}
	if definition.Path == "help" && len(result.Positionals) > 1 {
		result.Positionals = []string{strings.Join(result.Positionals, " ")}
	}
	requiredArguments := 0
	for _, argument := range definition.Arguments {
		if argument.Required {
			requiredArguments++
		}
	}
	if len(result.Positionals) < requiredArguments || len(result.Positionals) > len(definition.Arguments) {
		return result, usageError(fmt.Sprintf("%s expects %d..%d positional arguments", definition.Path, requiredArguments, len(definition.Arguments)))
	}
	for _, option := range definition.Options {
		if option.Required && !option.Boolean && len(result.Values[option.Name]) == 0 {
			return result, usageError("missing required option --" + option.Name)
		}
	}
	return result, nil
}

func matchDefinition(args []string) (CommandDefinition, []string, bool) {
	var best CommandDefinition
	bestWords := 0
	for _, definition := range commandDefinitions {
		pathWords := strings.Fields(definition.Path)
		if len(pathWords) > len(args) || len(pathWords) <= bestWords {
			continue
		}
		matches := true
		for index := range pathWords {
			if pathWords[index] != args[index] {
				matches = false
				break
			}
		}
		if matches {
			best, bestWords = definition, len(pathWords)
		}
	}
	if bestWords == 0 {
		return CommandDefinition{}, nil, false
	}
	return best, args[bestWords:], true
}

func (invocation invocation) Value(name string) string {
	values := invocation.Values[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func usageError(message string) *protocol.Error {
	return protocol.NewError(protocol.ErrUsageInvalid, protocol.CategoryUsage, message)
}
