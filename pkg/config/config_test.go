package config_test

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/test"
	"github.com/conductorone/baton-sdk/pkg/ustrings"
	"github.com/conductorone/baton-zoom/pkg/config"
)

func TestConfigs(t *testing.T) {
	test.ExerciseTestCasesFromExpressions(
		t,
		config.Config,
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
