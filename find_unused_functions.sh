#!/bin/bash

# Functions to check (exported functions from non-test files)
functions=(
    "BESizeFromPathLen"
    "CompositeEntryProcessor"
    "CreateDeletedTestData"
    "CreateEntryFromOffset"
    "CreateTestData"
    "DefaultEntryProcessor"
    "DefaultValidationConfig"
    "DetectEntryCorruption"
    "DiagnosticValidationProcessor"
    "FindRepositoryRootFrom"
    "FormatHumanRate"
    "FormatHumanSize"
    "GetDebugEnabled"
    "GetHashAlgorithm"
    "GetHashAlgorithmByType"
    "GetHashSize"
    "GetStringCopyStats"
    "GetVerbose"
    "GetVerboseLevel"
    "HashFile"
    "HashFileInterruptible"
    "HashFileToHexString"
    "HashStringToHexString"
    "HashTypeFromName"
    "HashTypeName"
    "IdxckValidationProcessor"
    "InitDebugFlags"
    "IsDebugEnabled"
    "IterateIndexFile"
    "LoadConfig"
    "LoadIndexFileMmap"
    "LogDebugFlags"
    "NewBEIndexFileIOEntry"
    "NewBEIndexFileMmapEntry"
    "NewBEScanEntry"
    "NewBESkiplistEntry"
    "NewBinaryEntryBase"
    "NewBinaryEntrySkiplistIterator"
    "NewDirectoryCache"
    "NewDupesCallback"
    "NewEnhancedFilesystemScanIterator"
    "NewFilesystemScanIterator"
    "NewIgnoreManager"
    "NewParsedOptions"
    "NewSafeEntryAccessor"
    "NewSkiplistIterator"
    "NewSkiplistWrapper"
    "NewSnapshotRepository"
    "NewStatusCallback"
    "NewTempIndexWriter"
    "NewUnifiedFilesystemScanIterator"
    "NewUpdateCallback"
    "NewValidatedEntry"
    "ParseHumanSize"
    "RecoveryValidationProcessor"
    "ResetStringCopyStats"
    "ResolveIndexFile"
    "SearchEntryProcessor"
    "SetDebugFlags"
    "SetVerboseLevel"
    "TimeFromWall"
    "TimeToWall"
    "UnifiedValidationProcessor"
    "ValidateDebugFlags"
    "ValidateEntryInfo"
    "ValidateHashAlgorithm"
    "ValidateHashWorkers"
    "ValidateIndexHeader"
    "ValidateIndexHeaderWithOptions"
    "ValidateIndexLockTimeout"
    "ValidateOutputFormat"
    "ValidateSymlinkMode"
    "ValidateVerboseLevel"
    "ValidationConfigWithFixes"
    "VerboseEnter"
    "VerboseEntryProcessor"
    "VerboseLog"
    "VerifyEntryChecksum"
)

unused_functions=()

for func in "${functions[@]}"; do
    # Search for function calls (not including the definition)
    calls=$(grep -r "${func}(" . --include="*.go" | grep -v "^[^:]*:func ${func}(" | grep -v "_test.go:" | wc -l)
    
    if [ "$calls" -eq 0 ]; then
        unused_functions+=("$func")
        echo "UNUSED: $func"
    else
        echo "USED: $func ($calls calls)"
    fi
done

echo
echo "Summary of unused functions:"
for func in "${unused_functions[@]}"; do
    echo "  $func"
done