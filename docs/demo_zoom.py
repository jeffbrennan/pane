# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow"]
# ///
"""Turn the vhs frames from docs/demo.tape into docs/demo.gif, easing between
zoomed views and showing each annotated keypress in a strip below the terminal.
The terminal sits in a drawn window, and the views move over the whole scene,
like a camera.

Usage: uv run docs/demo_zoom.py docs/demo.tape .scratch/demo-frames docs/demo.gif

vhs writes each frame as transparent text and cursor layers, so they are laid
on the theme background here, then set in a macOS-style window: title bar,
traffic lights, rounded corners and a shadow.

Views come from `# view: x y width [ease]` comments in the tape, as fractions
of the scene; the height follows the frame's aspect ratio. Keypresses come
from `# key: keys = description` comments. Both take effect at the next
command, so their times are worked out from the tape's Sleep and typing
durations, counting only the recorded (shown) part, then scaled to the
recording's real length, which vhs keeps a little shorter than the tape's
nominal time. The last moment fades back to the first frame so the loop
doesn't jump.
"""

import re
import shlex
import subprocess
import sys
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

EASE = 0.7  # seconds to move to a view, unless the tape gives one
SOURCE_FPS = 30  # the tape's Framerate
FPS = 30  # every recorded frame, for smooth zooms
SIZE = (1600, 900)  # the tape's Width and Height
BACKGROUND = "#282828"
OUT_WIDTH = 1080  # keeps the terminal itself at 1000 pixels

# The window, in source pixels around the terminal.
MARGIN = (64, 56, 64, 40)  # left, top, right, bottom
TITLE_BAR = 52
RADIUS = 20
OUTSIDE = "#1d2021"
TITLE_BAR_FILL = "#32302f"
EDGE = "#45403d"
LIGHTS = ("#ff5f57", "#febc2e", "#28c840")
TITLE = "~/ghostty"

STRIP = 60  # output pixels below the terminal for the keypresses
KEY_HOLD = 1.6  # seconds a keypress stays up, unless the next one replaces it
KEY_LEAD = 0.1  # show it just before the screen reacts, as a viewer reads it
KEY_FADE = 0.12
LOOP_FADE = 0.5
KEY_NAMES = {"enter": "↵", "tab": "tab"}
FONT = Path.home() / "Library/Fonts/ObelMonoMasked[wght].ttf"


def ease(t):
    return t * t * (3 - 2 * t)


def view(seconds, views):
    current = (0.0, 0.0, 1.0)
    for start, shot, length in views:
        if seconds < start:
            break
        progress = ease(min(1.0, (seconds - start) / length))
        current = tuple(a + (b - a) * progress for a, b in zip(current, shot))
    return current


def duration(text):
    value = float(re.match(r"[\d.]+", text).group())
    return value / 1000 if text.endswith("ms") else value


def cues(tape):
    """Return (seconds, keys, description) for each `# key:` comment,
    (seconds, (x, y, width), ease) for each `# view:` comment, and the tape's
    total shown time."""
    speed, seconds, shown = 0.05, 0.0, False
    pending, presses, views = [], [], []
    for line in Path(tape).read_text().splitlines():
        line = line.strip()
        if line.startswith("# key:"):
            keys, _, description = line.removeprefix("# key:").partition("=")
            pending.append((presses, keys.split(), description.strip()))
            continue
        if line.startswith("# view:"):
            x, y, width, *length = map(float, line.removeprefix("# view:").split())
            pending.append((views, (x, y, width), length[0] if length else EASE))
            continue
        if not line or line.startswith("#"):
            continue
        command, *args = shlex.split(line)
        name, _, at = command.partition("@")
        if name == "Set" and args[0] == "TypingSpeed":
            speed = duration(args[1])
        elif name in ("Hide", "Show"):
            shown = name == "Show"
        if not shown or name in ("Set", "Show", "Output"):
            continue
        for target, *cue in pending:
            target.append((seconds, *cue))
        pending = []
        step = duration(at) if at else speed
        if name == "Sleep":
            seconds += duration(args[0])
        elif name == "Type":
            seconds += step * len(args[0])
        else:
            seconds += step * (int(args[0]) if args else 1)
    return presses, views, seconds


def font(size, weight):
    try:
        face = ImageFont.truetype(str(FONT), size)
        face.set_variation_by_axes([weight])
        return face
    except OSError:
        return ImageFont.load_default(size)


def overlay(frame, seconds, presses):
    """Draw the keypress showing at this time, if any, in the bottom strip."""
    for index, (start, keys, description) in enumerate(presses):
        start -= KEY_LEAD
        end = start + KEY_HOLD
        if index + 1 < len(presses):
            end = min(end, presses[index + 1][0] - KEY_LEAD)
        if start <= seconds < end:
            break
    else:
        return frame
    alpha = min(1.0, (seconds - start) / KEY_FADE, (end - seconds) / KEY_FADE)
    key_font, text_font = font(20, 700), font(18, 400)
    layer = Image.new("RGBA", frame.size)
    draw = ImageDraw.Draw(layer)
    caps = [KEY_NAMES.get(key, key) for key in keys]
    cap_widths = [max(28, draw.textlength(cap, font=key_font) + 18) for cap in caps]
    text_width = draw.textlength(description, font=text_font)
    width = sum(cap_widths) + 6 * (len(caps) - 1) + 14 + text_width + 32
    left, top = (frame.width - width) / 2, frame.height - STRIP / 2 - 23
    draw.rounded_rectangle((left, top, left + width, top + 46), 12,
                           fill=(50, 48, 47, 245), outline=(80, 73, 69, 255), width=1)
    x = left + 16
    for cap, cap_width in zip(caps, cap_widths):
        draw.rounded_rectangle((x, top + 8, x + cap_width, top + 38), 7,
                               fill=(60, 56, 54, 255), outline=(124, 111, 100, 255), width=1)
        draw.text((x + cap_width / 2, top + 23), cap, font=key_font, fill=(251, 241, 199, 255), anchor="mm")
        x += cap_width + 6
    draw.text((x + 8, top + 23), description, font=text_font, fill=(213, 196, 161, 255), anchor="lm")
    layer.putalpha(layer.getchannel("A").point(lambda a: round(a * alpha)))
    return Image.alpha_composite(frame.convert("RGBA"), layer).convert("RGB")


def window():
    """Return the scene without the terminal, where the terminal goes, and the
    mask that rounds its bottom corners."""
    width, height = SIZE
    left, top, right, bottom = MARGIN
    scene = Image.new("RGB", (left + width + right, top + TITLE_BAR + height + bottom), OUTSIDE)
    outline = (left, top, left + width - 1, top + TITLE_BAR + height - 1)
    shadow = Image.new("L", scene.size)
    ImageDraw.Draw(shadow).rounded_rectangle((outline[0], outline[1] + 14, outline[2], outline[3] + 14),
                                             RADIUS, fill=150)
    scene.paste("black", mask=shadow.filter(ImageFilter.GaussianBlur(22)))
    draw = ImageDraw.Draw(scene)
    draw.rounded_rectangle(outline, RADIUS, fill=TITLE_BAR_FILL, outline=EDGE, width=2)
    draw.line((left + 2, top + TITLE_BAR - 1, left + width - 3, top + TITLE_BAR - 1), fill=EDGE)
    middle = top + TITLE_BAR / 2
    for index, color in enumerate(LIGHTS):
        x = left + 30 + index * 30
        draw.ellipse((x - 9, middle - 9, x + 9, middle + 9), fill=color)
    draw.text((left + width / 2, middle), TITLE, font=font(20, 500), fill="#a89984", anchor="mm")
    mask = Image.new("L", SIZE)
    ImageDraw.Draw(mask).rounded_rectangle((2, -RADIUS, width - 3, height - 3), RADIUS - 2, fill=255)
    return scene, (left, top + TITLE_BAR), mask


def compose(frames, number):
    frame = Image.new("RGB", SIZE, BACKGROUND)
    for layer in ("cursor", "text"):
        image = Image.open(frames / f"frame-{layer}-{number:05d}.png")
        offset = ((SIZE[0] - image.width) // 2, (SIZE[1] - image.height) // 2)
        frame.paste(image, offset, image)
    return frame


def main(tape, source, target):
    frames = Path(source)
    count = len(list(frames.glob("frame-text-*.png")))
    presses, views, total = cues(tape)
    scale = count / SOURCE_FPS / total
    if scale < 0.9:
        sys.exit(f"vhs recorded {count / SOURCE_FPS:.1f}s of a {total:.1f}s tape; "
                 "it drops frames when the machine is busy, so record again when it's idle")
    presses = [(start * scale, *rest) for start, *rest in presses]
    views = [(start * scale, *rest) for start, *rest in views]
    scene, corner, mask = window()
    width, height = scene.size
    scene_height = round(OUT_WIDTH * height / width / 2) * 2
    out_height = scene_height + STRIP
    frame_count = (count - 1) * FPS // SOURCE_FPS + 1
    writer = subprocess.Popen(
        ["ffmpeg", "-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "rgb24",
         "-s", f"{OUT_WIDTH}x{out_height}", "-r", str(FPS), "-i", "-",
         "-vf", "split[a][b];[a]palettegen=max_colors=64:stats_mode=full[p];"
         "[b][p]paletteuse=dither=none:diff_mode=rectangle",
         target], stdin=subprocess.PIPE)
    assert writer.stdin
    first = None
    for written in range(frame_count):
        seconds = written / FPS
        x, y, w = view(seconds, views)
        box = (x * width, y * height, (x + w) * width, (y + w) * height)
        number = round(written * SOURCE_FPS / FPS) + 1
        shot = scene.copy()
        shot.paste(compose(frames, number), corner, mask)
        frame = Image.new("RGB", (OUT_WIDTH, out_height), OUTSIDE)
        frame.paste(shot.resize((OUT_WIDTH, scene_height), Image.LANCZOS, box=box))
        frame = overlay(frame, seconds, presses)
        first = first or frame
        remaining = (frame_count - 1 - written) / FPS
        if remaining < LOOP_FADE:
            frame = Image.blend(first, frame, ease(remaining / LOOP_FADE))
        writer.stdin.write(frame.tobytes())
    written = frame_count
    writer.stdin.close()
    if writer.wait():
        sys.exit("ffmpeg failed")
    print(f"{target}: {written} frames, {Path(target).stat().st_size / 1e6:.1f} MB")


if __name__ == "__main__":
    main(*sys.argv[1:4])
