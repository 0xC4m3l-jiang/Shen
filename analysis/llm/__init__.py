"""`llm-components`（L4）：契约 / 超时 / 内容纪律与间接注入防护。

规则落点：`AR-15`…`AR-24` · `AR-31` · `AR-32`（见 `docs/modules/llm-components.md`）。
"""

from analysis.llm.blacklist import Blacklist, Finding
from analysis.llm.client import AnalysisClient, UnconfiguredClient, assert_no_execution_surface
from analysis.llm.contract import ContractError, Field, Schema, validate
from analysis.llm.envelope import Envelope, accept, reject
from analysis.llm.extract import ExtractionError, extract_json
from analysis.llm.limits import LIST_CAP, PURPOSE_LIMITS, Truncation, truncate_list
from analysis.llm.prompts import PromptResourceError, PromptStore, assert_startup
from analysis.llm.twophase import TwoPhaseResult, run_two_phase
from analysis.llm.untrusted import as_data_block, assert_structured

__all__ = [
    "LIST_CAP",
    "PURPOSE_LIMITS",
    "AnalysisClient",
    "Blacklist",
    "ContractError",
    "Envelope",
    "ExtractionError",
    "Field",
    "Finding",
    "PromptResourceError",
    "PromptStore",
    "Schema",
    "Truncation",
    "TwoPhaseResult",
    "UnconfiguredClient",
    "accept",
    "as_data_block",
    "assert_no_execution_surface",
    "assert_startup",
    "assert_structured",
    "extract_json",
    "reject",
    "run_two_phase",
    "truncate_list",
    "validate",
]
