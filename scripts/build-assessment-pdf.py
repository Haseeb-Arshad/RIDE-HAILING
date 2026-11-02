from __future__ import annotations

import html
import re
from pathlib import Path

from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (
    BaseDocTemplate, Frame, PageBreak, PageTemplate, Paragraph, Spacer,
)

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "docs" / "TECHNICAL_ASSESSMENT_RESPONSE.md"
OUTPUT = ROOT / "output" / "pdf" / "technical-assessment-response.pdf"


def document_font() -> str:
    font_dir = Path("C:/Windows/Fonts")
    files = {
        "Arial": "arial.ttf",
        "Arial-Bold": "arialbd.ttf",
        "Arial-Italic": "ariali.ttf",
        "Arial-BoldItalic": "arialbi.ttf",
    }
    if all((font_dir / filename).exists() for filename in files.values()):
        for name, filename in files.items():
            pdfmetrics.registerFont(TTFont(name, str(font_dir / filename)))
        pdfmetrics.registerFontFamily(
            "Arial", normal="Arial", bold="Arial-Bold",
            italic="Arial-Italic", boldItalic="Arial-BoldItalic",
        )
        return "Arial"
    return "Helvetica"


def inline_markup(value: str) -> str:
    value = re.sub("[\u2010-\u2014\u2212]", "-", value.strip())
    token = re.compile(r"\[([^\]]+)\]\(([^)]+)\)|\x60([^\x60]+)\x60|\*\*([^*]+)\*\*")
    parts = []
    cursor = 0
    for match in token.finditer(value):
        parts.append(html.escape(value[cursor:match.start()], quote=False))
        label, url, code, bold = match.groups()
        if label is not None:
            safe_label = html.escape(label, quote=False)
            if url.startswith(("https://", "http://")):
                parts.append(f'<link href="{html.escape(url, quote=True)}" color="#000000">{safe_label}</link>')
            else:
                parts.append(safe_label)
        elif code is not None:
            parts.append(html.escape(code, quote=False))
        else:
            parts.append(f"<b>{html.escape(bold, quote=False)}</b>")
        cursor = match.end()
    parts.append(html.escape(value[cursor:], quote=False))
    return "".join(parts)


def make_styles(font: str):
    def style(name, size, leading, **kwargs):
        return ParagraphStyle(
            name, fontName=font, fontSize=size, leading=leading,
            textColor=colors.black, **kwargs,
        )
    return {
        "title": ParagraphStyle(
            "Title", fontName=font + "-Bold", fontSize=20, leading=25,
            textColor=colors.black, spaceAfter=7,
        ),
        "subtitle": style("Subtitle", 13, 18, spaceAfter=16),
        "meta": style("Meta", 10, 14, spaceAfter=4),
        "section": ParagraphStyle(
            "Section", fontName=font + "-Bold", fontSize=13, leading=18,
            textColor=colors.black, spaceBefore=15, spaceAfter=7, keepWithNext=True,
        ),
        "question": ParagraphStyle(
            "Question", fontName=font + "-Bold", fontSize=11, leading=15,
            textColor=colors.black, spaceBefore=13, spaceAfter=7, keepWithNext=True,
        ),
        "body": style("Body", 11, 15, spaceAfter=8),
        "bullet": style("Bullet", 11, 15, leftIndent=13, firstLineIndent=-10, spaceAfter=7),
    }


def parse_markdown(source: str, styles: dict):
    lines = source.splitlines()
    story = []
    i = 0
    while i < len(lines):
        line = lines[i].strip()
        i += 1
        if not line:
            continue
        if line.startswith("# "):
            story.append(Paragraph(inline_markup(line[2:]), styles["title"]))
        elif line.startswith("## "):
            story.append(Paragraph(inline_markup(line[3:]), styles["subtitle"]))
        elif line.startswith(("**Assessment:**", "**Review date:**")):
            story.append(Paragraph(inline_markup(line), styles["meta"]))
            if line.startswith("**Review date:**"):
                story.append(Spacer(1, 5 * mm))
        elif line.startswith("#### "):
            if line.startswith(("#### 4.", "#### 7.")):
                story.append(PageBreak())
            story.append(Paragraph(inline_markup(line[5:]), styles["question"]))
        elif line.startswith("### "):
            if line in {"### Architecture questions", "### Verification"}:
                story.append(PageBreak())
            story.append(Paragraph(inline_markup(line[4:]), styles["section"]))
        elif line.startswith("- ") or re.match(r"^\d+\.\s", line):
            if line.startswith("- "):
                prefix, text = "&#8226;", line[2:]
            else:
                match = re.match(r"^(\d+\.)\s+(.*)$", line)
                prefix, text = match.groups()
            story.append(Paragraph(f"{prefix} {inline_markup(text)}", styles["bullet"]))
        else:
            paragraph = [line]
            while i < len(lines) and lines[i].strip() and not lines[i].strip().startswith(("#", "- ")):
                paragraph.append(lines[i].strip())
                i += 1
            story.append(Paragraph(inline_markup(" ".join(paragraph)), styles["body"]))
    return story


def main():
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    font = document_font()
    doc = BaseDocTemplate(
        str(OUTPUT), pagesize=A4, leftMargin=23 * mm, rightMargin=23 * mm,
        topMargin=21 * mm, bottomMargin=21 * mm,
        title="Lahore Ride-Hailing - Technical Assessment Response",
        author="Lahore Ride-Hailing",
    )

    def footer(canvas, document):
        canvas.saveState()
        canvas.setFont(font, 9)
        canvas.setFillColor(colors.black)
        canvas.drawString(document.leftMargin, 12 * mm, "Lahore Ride-Hailing | Technical Assessment")
        canvas.drawRightString(A4[0] - document.rightMargin, 12 * mm, str(document.page))
        canvas.restoreState()

    frame = Frame(
        doc.leftMargin, doc.bottomMargin, doc.width, doc.height, id="main",
        leftPadding=0, rightPadding=0, topPadding=0, bottomPadding=0,
    )
    doc.addPageTemplates([PageTemplate(id="assessment", frames=frame, onPage=footer)])
    doc.build(parse_markdown(SOURCE.read_text(encoding="utf-8-sig"), make_styles(font)))
    print(f"Generated {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
