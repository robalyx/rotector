// Code generated by "enumer -type=ReviewMode -trimprefix=ReviewMode"; DO NOT EDIT.

package enum

import (
	"fmt"
	"strings"
)

const _ReviewModeName = "TrainingStandard"

var _ReviewModeIndex = [...]uint8{0, 8, 16}

const _ReviewModeLowerName = "trainingstandard"

func (i ReviewMode) String() string {
	if i < 0 || i >= ReviewMode(len(_ReviewModeIndex)-1) {
		return fmt.Sprintf("ReviewMode(%d)", i)
	}
	return _ReviewModeName[_ReviewModeIndex[i]:_ReviewModeIndex[i+1]]
}

// An "invalid array index" compiler error signifies that the constant values have changed.
// Re-run the stringer command to generate them again.
func _ReviewModeNoOp() {
	var x [1]struct{}
	_ = x[ReviewModeTraining-(0)]
	_ = x[ReviewModeStandard-(1)]
}

var _ReviewModeValues = []ReviewMode{ReviewModeTraining, ReviewModeStandard}

var _ReviewModeNameToValueMap = map[string]ReviewMode{
	_ReviewModeName[0:8]:       ReviewModeTraining,
	_ReviewModeLowerName[0:8]:  ReviewModeTraining,
	_ReviewModeName[8:16]:      ReviewModeStandard,
	_ReviewModeLowerName[8:16]: ReviewModeStandard,
}

var _ReviewModeNames = []string{
	_ReviewModeName[0:8],
	_ReviewModeName[8:16],
}

// ReviewModeString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func ReviewModeString(s string) (ReviewMode, error) {
	if val, ok := _ReviewModeNameToValueMap[s]; ok {
		return val, nil
	}

	if val, ok := _ReviewModeNameToValueMap[strings.ToLower(s)]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to ReviewMode values", s)
}

// ReviewModeValues returns all values of the enum
func ReviewModeValues() []ReviewMode {
	return _ReviewModeValues
}

// ReviewModeStrings returns a slice of all String values of the enum
func ReviewModeStrings() []string {
	strs := make([]string, len(_ReviewModeNames))
	copy(strs, _ReviewModeNames)
	return strs
}

// IsAReviewMode returns "true" if the value is listed in the enum definition. "false" otherwise
func (i ReviewMode) IsAReviewMode() bool {
	for _, v := range _ReviewModeValues {
		if i == v {
			return true
		}
	}
	return false
}