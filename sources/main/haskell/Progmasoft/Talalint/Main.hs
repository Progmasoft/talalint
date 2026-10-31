-- SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
-- SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

module Main (main) where

import Control.Monad (when)
import Data.List.NonEmpty qualified as NonEmpty
import Data.Text qualified as Text
import Data.Text.IO qualified as TextIO
import System.Environment (getArgs)
import Data.Version (showVersion)
import Paths_talalint (version)
import System.Exit (ExitCode (..), exitFailure, exitWith)
import System.IO
    ( Handle
    , IOMode (..)
    , hPutStrLn
    , hSetEncoding
    , hSetNewlineMode
    , noNewlineTranslation
    , stderr
    , utf8
    , withFile
    )
import Progmasoft.Talalint
import Progmasoft.Talalint.CommandLine
import Progmasoft.Tala.AST
import Progmasoft.Tala.Frontend

main :: IO ()
main = do
    arguments <- getArgs
    case parseCommandLine arguments of
        Left problem -> do
            hPutStrLn stderr ("talalint: " ++ renderCommandLineError problem)
            exitWith (ExitFailure usageError)
        Right ShowHelp -> putStr usageText
        Right ShowVersion -> putStrLn ("talalint " ++ showVersion version)
        Right (Lint mode paths) -> do
            clean <- mapM (lintPath mode) (NonEmpty.toList paths)
            if and clean then pure () else exitFailure

-- | Exit status of a command line that could not be read.
usageError :: Int
usageError = 2

-- | Lint one file; True when nothing is left to report.
lintPath :: Mode -> FilePath -> IO Bool
lintPath mode path = do
    source <- Text.unpack <$> withFile path ReadMode (\handle -> asWritten handle >> TextIO.hGetContents handle)
    finalSource <- case mode of
        Fix -> applyFixes path source
        Report -> pure source
    let problems = lintSource (CompilerInput path finalSource)
    mapM_ printDiagnostic problems
    pure (null problems)

applyFixes :: FilePath -> String -> IO String
applyFixes path source = do
    let fixed = applySafeFixes source
    when (fixed /= source) $
        withFile path WriteMode (\handle -> asWritten handle >> TextIO.hPutStr handle (Text.pack fixed))
    pure fixed

{- | Reads and writes UTF-8 with the line endings as they are. The platform's
text mode would turn CRLF into LF on Windows and hide the line endings that
the checks look at.
-}
asWritten :: Handle -> IO ()
asWritten handle = hSetEncoding handle utf8 >> hSetNewlineMode handle noNewlineTranslation

printDiagnostic :: LintDiagnostic -> IO ()
printDiagnostic problem =
    putStrLn
        ( renderLocation (lintSpan problem)
            ++ renderSeverity (lintSeverity problem)
            ++ " ["
            ++ lintRuleId problem
            ++ "] "
            ++ lintMessage problem
        )

renderLocation :: Maybe SourceSpan -> String
renderLocation Nothing = ""
renderLocation (Just spanValue) =
    sourceFile spanValue
        ++ ":"
        ++ show (sourceLine (sourceStart spanValue))
        ++ ":"
        ++ show (sourceColumn (sourceStart spanValue))
        ++ ": "

renderSeverity :: LintSeverity -> String
renderSeverity severity = case severity of
    LintInfo -> "info"
    LintWarning -> "warning"
    LintError -> "error"
