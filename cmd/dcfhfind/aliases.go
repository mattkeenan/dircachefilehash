package main

import dcfh "github.com/mattkeenan/dircachefilehash/pkg"

// Thin aliases so existing parser code and tests can keep using the local
// names Expression/Action/NameTest/... after the filter DSL moved to
// pkg/filter.go. These are zero-cost — Go treats them as identical types.
type (
	Expression     = dcfh.FilterExpr
	Action         = dcfh.FilterAction
	EvalContext    = dcfh.FilterContext
	NameTest       = dcfh.NameTest
	PathTest       = dcfh.PathTest
	SizeTest       = dcfh.SizeTest
	EmptyTest      = dcfh.EmptyTest
	DeletedTest    = dcfh.DeletedTest
	ValidTest      = dcfh.ValidTest
	CorruptTest    = dcfh.CorruptTest
	HashTest       = dcfh.HashTest
	HashPrefixTest = dcfh.HashPrefixTest
	HashTypeTest   = dcfh.HashTypeTest
	MTimeTest      = dcfh.MTimeTest
	MMinTest       = dcfh.MMinTest
	CTimeTest      = dcfh.CTimeTest
	CMinTest       = dcfh.CMinTest
	AndExpression  = dcfh.AndExpression
	OrExpression   = dcfh.OrExpression
	NotExpression  = dcfh.NotExpression
	PrintAction    = dcfh.PrintAction
	Print0Action   = dcfh.Print0Action
	LsAction       = dcfh.LsAction
	PrintfAction   = dcfh.PrintfAction
	ValidateAction = dcfh.ValidateAction
	ChecksumAction = dcfh.ChecksumAction
	FixAction      = dcfh.FixAction
)
