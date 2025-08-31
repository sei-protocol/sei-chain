import re
import datetime
from pathlib import Path
from typing import Dict


def parse_bill(text: str) -> Dict:
    # Basic regex-based parser (replace with LLM later)
    amount = re.search(r"\$([0-9]+\.[0-9]{2})", text)
    due_date = re.search(r"Due(?:\sDate)?:\s*(\d{2}/\d{2}/\d{4})", text)

    return {
        "payee": "UtilityCompanyUSDCAddress",  # Replace with extraction
        "amount": float(amount.group(1)) if amount else None,
        "due_date": (
            datetime.datetime.strptime(due_date.group(1), "%m/%d/%Y").timestamp()
            if due_date
            else None
        ),
    }


if __name__ == "__main__":
    bill_text = Path("example_bill.txt").read_text()
    parsed = parse_bill(bill_text)
    print(parsed)
