package canonical

import (
	"fmt"
)

// Instruction preserves one ordered current-invocation directive block.
type Instruction struct {
	role MessageRole
	text string
}

func NewInstruction(role MessageRole, text string) (Instruction, error) {
	if role != MessageRoleSystem && role != MessageRoleDeveloper {
		return Instruction{}, fmt.Errorf("canonical instruction role %q is invalid", role)
	}
	return Instruction{role: role, text: text}, nil
}

func (i Instruction) Role() MessageRole { return i.role }
func (i Instruction) Clone() Instruction {
	cloned, _ := NewInstruction(i.role, i.text)
	return cloned
}
func (i Instruction) Text() string { return i.text }

// InstructionSet preserves directive role and block order.
type InstructionSet struct{ ordered []Instruction }

func NewInstructionSet(instructions []Instruction) (InstructionSet, error) {
	set := InstructionSet{ordered: make([]Instruction, len(instructions))}
	for i := range instructions {
		if instructions[i].Role() != MessageRoleSystem && instructions[i].Role() != MessageRoleDeveloper {
			return InstructionSet{}, fmt.Errorf("canonical instruction set contains an invalid instruction")
		}
		set.ordered[i] = instructions[i].Clone()
	}
	return set, nil
}

func NewSystemInstructionSet(text string) InstructionSet {
	instruction := Instruction{role: MessageRoleSystem, text: text}
	set, _ := NewInstructionSet([]Instruction{instruction})
	return set
}
func (s InstructionSet) Instructions() []Instruction {
	out := make([]Instruction, len(s.ordered))
	for i := range s.ordered {
		out[i] = s.ordered[i].Clone()
	}
	return out
}
func (s InstructionSet) IsEmpty() bool { return len(s.ordered) == 0 }
func (s InstructionSet) Clone() InstructionSet {
	cloned, _ := NewInstructionSet(s.ordered)
	return cloned
}
