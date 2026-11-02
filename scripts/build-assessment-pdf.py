from __future__ import annotations

import html
import re
from pathlib import Path

from reportlab.lib import colors
from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import mm
from reportlab.platypus import (
    BaseDocTemplate,
    Frame,
    KeepTogether,
    PageTemplate,
    Paragraph,
    Spacer,
    Table,
    TableStyle,
)


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "docs" / "TECHNICAL_ASSESSMENT_RESPONSE.md"
OUTPUT = ROOT / "output" / "pdf" / "technical-assessment-response.pdf"

INK = colors.HexColor("#26352F")
GREEN = colors.HexColor("#245642")
GOLD = colors.HexColor("#D3A44D")
MUTED = colors.HexColor("#64736B")
PAPER = colors.HexColor("#F5F7F3")
LINE = colors.HexColor("#D9E1DA")


def normalize_text(value: str) -> str:
    return (
        value.replace("\u2010", "-")
        .replace("\u2011", "-")
        .replace("\u2012", "-")
        .replace("\u2013", "-")
        .replace("\u2014", "-")
        .replace("\u2212", "-")
    )


def inline_markup(value: str) -> str:
    value = normalize_text(value.strip())
    parts: list[str] = []
    cursor = 0
    token = re.compile(r"\[([^\]]+)\]\(([^)]+)\)|`([^`]+)`|\*\*([^*]+)\*\*|\*([^*]+)\*")
    for match in token.finditer(value):
        parts.append(html.escape(value[cursor : match.start()], quote=False))
        label, url, code, bold, italic = match.groups()
        if label is not None:
            safe_label = html.escape(label, quote=False)
            if url.startswith("https://") or url.startswith("http://"):
                safe_url = html.escape(url, quote=True)
                parts.append(f'<link href="{safe_url}" color="#245642">{safe_label}</link>')
            else:
                parts.append(safe_label)
        elif code is not None:
            parts.append(f'<font name="Courier" size="7.6">{html.escape(code, quote=False)}</font>')
        elif bold is not None:
            parts.append(f"<b>{html.escape(bold, quote=False)}</b>")
        elif italic is not None:
            parts.append(f"<i>{html.escape(italic, quote=False)}</i>")
        cursor = match.end()
    parts.append(html.escape(value[cursor:], quote=False))
    return "".join(parts)


def make_styles():
    base = getSampleStyleSheet()
    return {
        "title": ParagraphStyle(
            "ProjectTitle", parent=base["Title"], fontName="Helvetica-Bold", fontSize=21,
            leading=25, textColor=GREEN, alignment=TA_LEFT, spaceAfter=3,
        ),
        "subtitle": ParagraphStyle(
            "ProjectSubtitle", parent=base["Normal"], fontName="Helvetica", fontSize=10,
            leading=14, textColor=MUTED, spaceAfter=9,
        ),
        "meta": ParagraphStyle(
            "Meta", parent=base["Normal"], fontName="Helvetica", fontSize=8,
            leading=11, textColor=INK,
        ),
        "h2": ParagraphStyle(
            "Section", parent=base["Heading2"], fontName="Helvetica-Bold", fontSize=11.8,
            leading=14.2, textColor=GREEN, spaceBefore=9, spaceAfter=4, keepWithNext=True,
        ),
        "h3": ParagraphStyle(
            "Subsection", parent=base["Heading3"], fontName="Helvetica-Bold", fontSize=9.4,
            leading=11.5, textColor=INK, spaceBefore=6, spaceAfter=3, keepWithNext=True,
        ),
        "body": ParagraphStyle(
            "Body", parent=base["BodyText"], fontName="Helvetica", fontSize=8.2,
            leading=10.8, textColor=INK, spaceAfter=4,
        ),
        "bullet": ParagraphStyle(
            "Bullet", parent=base["BodyText"], fontName="Helvetica", fontSize=8,
            leading=10, textColor=INK, leftIndent=12, firstLineIndent=-8, spaceAfter=2.5,
        ),
        "table": ParagraphStyle(
            "TableCell", parent=base["BodyText"], fontName="Helvetica", fontSize=7.1,
            leading=8.8, textColor=INK, spaceAfter=0,
        ),
        "table_header": ParagraphStyle(
            "TableHeader", parent=base["BodyText"], fontName="Helvetica-Bold", fontSize=7.2,
            leading=8.8, textColor=colors.white, spaceAfter=0,
        ),
    }


def parse_markdown(source: str, styles: dict[str, ParagraphStyle]) -> list:
    lines = source.splitlines()
    story: list = []
    title = "Lahore Ride-Hailing"
    subtitle = "Technical Assessment Response and Implementation Review"
    story.extend([
        Spacer(1, 5 * mm),
        Paragraph(title, styles["title"]),
        Paragraph(subtitle, styles["subtitle"]),
    ])
    meta_values = []
    for line in lines[:8]:
        if line.startswith("**Assessment:**") or line.startswith("**Review date:**"):
            meta_values.append(inline_markup(line.replace("**", "")))
    if meta_values:
        meta_table = Table([[Paragraph(item, styles["meta"]) for item in meta_values]], colWidths=[82 * mm, 76 * mm])
        meta_table.setStyle(TableStyle([
            ("BACKGROUND", (0, 0), (-1, -1), PAPER),
            ("BOX", (0, 0), (-1, -1), 0.5, LINE),
            ("VALIGN", (0, 0), (-1, -1), "MIDDLE"),
            ("LEFTPADDING", (0, 0), (-1, -1), 7),
            ("RIGHTPADDING", (0, 0), (-1, -1), 7),
            ("TOPPADDING", (0, 0), (-1, -1), 6),
            ("BOTTOMPADDING", (0, 0), (-1, -1), 6),
        ]))
        story.extend([meta_table, Spacer(1, 5 * mm)])

    i = 0
    while i < len(lines):
        line = lines[i].strip()
        if not line or line in {"---", "```"}:
            i += 1
            continue
        if line.startswith("# ") or line.startswith("## ") or line.startswith("**Assessment:**") or line.startswith("**Review date:**"):
            i += 1
            continue
        if line.startswith("|"):
            rows = []
            while i < len(lines) and lines[i].strip().startswith("|"):
                cells = [cell.strip() for cell in lines[i].strip().strip("|").split("|")]
                if cells and all(re.fullmatch(r"[:\- ]+", cell or "-") for cell in cells):
                    i += 1
                    continue
                rows.append(cells)
                i += 1
            if rows:
                column_count = max(len(row) for row in rows)
                data = []
                for row_index, row in enumerate(rows):
                    row += [""] * (column_count - len(row))
                    style = styles["table_header"] if row_index == 0 else styles["table"]
                    data.append([Paragraph(inline_markup(cell), style) for cell in row])
                widths = [30 * mm, 29 * mm, 105 * mm]
                if column_count == 2:
                    widths = [55 * mm, 109 * mm]
                elif column_count == 4:
                    widths = [36 * mm, 42 * mm, 54 * mm, 32 * mm]
                elif column_count == 5:
                    widths = [29 * mm, 31 * mm, 37 * mm, 55 * mm, 12 * mm]
                table = Table(data, colWidths=widths[:column_count], repeatRows=1, hAlign="LEFT", splitByRow=1)
                table.setStyle(TableStyle([
                    ("BACKGROUND", (0, 0), (-1, 0), GREEN),
                    ("ROWBACKGROUNDS", (0, 1), (-1, -1), [colors.white, PAPER]),
                    ("GRID", (0, 0), (-1, -1), 0.35, LINE),
                    ("VALIGN", (0, 0), (-1, -1), "TOP"),
                    ("LEFTPADDING", (0, 0), (-1, -1), 5),
                    ("RIGHTPADDING", (0, 0), (-1, -1), 5),
                    ("TOPPADDING", (0, 0), (-1, -1), 5),
                    ("BOTTOMPADDING", (0, 0), (-1, -1), 5),
                ]))
                story.extend([Spacer(1, 2 * mm), table, Spacer(1, 3 * mm)])
            continue
        if line.startswith("#### "):
            story.append(Paragraph(inline_markup(line[5:]), styles["h3"]))
            i += 1
            continue
        if line.startswith("### "):
            story.append(Paragraph(inline_markup(line[4:]), styles["h2"]))
            i += 1
            continue
        if line.startswith("- ") or re.match(r"^\d+\.\s", line):
            if line.startswith("- "):
                text = line[2:]
                bullet = "&#8226;"
            else:
                match = re.match(r"^(\d+)\.\s+(.*)$", line)
                bullet, text = match.group(1) + ".", match.group(2)
            story.append(Paragraph(f"{bullet} {inline_markup(text)}", styles["bullet"]))
            i += 1
            continue
        paragraph_lines = [line]
        i += 1
        while i < len(lines) and lines[i].strip() and not lines[i].strip().startswith(("#", "|", "- ")) and not re.match(r"^\d+\.\s", lines[i].strip()):
            paragraph_lines.append(lines[i].strip())
            i += 1
        story.append(Paragraph(inline_markup(" ".join(paragraph_lines)), styles["body"]))
    return story


def decorate(canvas, doc):
    canvas.saveState()
    width, height = A4
    canvas.setStrokeColor(GOLD)
    canvas.setLineWidth(2)
    canvas.line(doc.leftMargin, height - 13 * mm, width - doc.rightMargin, height - 13 * mm)
    canvas.setFont("Helvetica", 7.4)
    canvas.setFillColor(MUTED)
    canvas.drawString(doc.leftMargin, 8 * mm, "LAHORE RIDE-HAILING  /  TECHNICAL ASSESSMENT")
    canvas.drawRightString(width - doc.rightMargin, 8 * mm, f"{doc.page}")
    canvas.restoreState()


def main():
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    source = SOURCE.read_text(encoding="utf-8")
    styles = make_styles()
    doc = BaseDocTemplate(
        str(OUTPUT), pagesize=A4, leftMargin=22 * mm, rightMargin=22 * mm,
        topMargin=20 * mm, bottomMargin=17 * mm, title="Lahore Ride-Hailing Technical Assessment Response",
        author="Lahore Ride-Hailing",
    )
    frame = Frame(doc.leftMargin, doc.bottomMargin, doc.width, doc.height, id="main", leftPadding=0, rightPadding=0, topPadding=0, bottomPadding=0)
    doc.addPageTemplates([PageTemplate(id="assessment", frames=frame, onPage=decorate)])
    story = parse_markdown(source, styles)
    doc.build(story)
    print(f"Generated {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
