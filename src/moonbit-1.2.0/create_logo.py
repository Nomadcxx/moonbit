#!/usr/bin/env python3
"""Generate PNG logo from ASCII art with transparent background."""

from PIL import Image, ImageDraw, ImageFont
import sys

# ASCII art from ascii.txt
ascii_art = """
█▀▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ █▄  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀

"""

# Use a monospace font
try:
    # Try common monospace fonts
    font = ImageFont.truetype("/usr/share/fonts/liberation/LiberationMono-Regular.ttf", 20)
except:
    try:
        font = ImageFont.truetype("/usr/share/fonts/TTF/DejaVuSansMono.ttf", 20)
    except:
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 20)
        except:
            print("Warning: Could not load monospace font, using default")
            font = ImageFont.load_default()

# Calculate image size
lines = ascii_art.strip().split('\n')
max_width = max(len(line) for line in lines)

# Create test image to measure text
test_img = Image.new('RGBA', (1, 1), (255, 255, 255, 0))
test_draw = ImageDraw.Draw(test_img)

# Measure a character to get dimensions
bbox = test_draw.textbbox((0, 0), "█", font=font)
char_width = bbox[2] - bbox[0]
char_height = bbox[3] - bbox[1]

# Calculate final dimensions with padding
width = (max_width * char_width) + 40
height = (len(lines) * char_height) + 40

# Create image with transparent background
img = Image.new('RGBA', (width, height), (255, 255, 255, 0))
draw = ImageDraw.Draw(img)

# Draw each line
y_offset = 20
for line in lines:
    draw.text((20, y_offset), line, fill=(255, 255, 255, 255), font=font)
    y_offset += char_height

# Save
img.save('logo.png')
print(f"Created logo.png ({width}x{height})")
