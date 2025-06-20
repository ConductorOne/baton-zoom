package main

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/test"
	"github.com/conductorone/baton-sdk/pkg/ustrings"
)

func TestConfigs(t *testing.T) {
	test.ExerciseTestCasesFromExpressions(
		t,
		field.NewConfiguration(ConfigurationFields),
		nil,
		ustrings.ParseFlags,
		[]test.TestCaseFromExpression{
			{
				"",
				false,
				"empty",
			},
			{
				"--client-id 1 --zoom-client-secret 1",
				false,
				"account id missing",
			},
			{
				"--account-id 1 --zoom-client-secret 1",
				false,
				"client-id missing",
			},
			{
				"--account-id 1 --zoom-client-id 1",
				false,
				"client id missing",
			},
			{
				"--account-id 1 --zoom-client-id 1 --zoom-client-secret 1",
				true,
				"all",
			},
		},
	)
}
