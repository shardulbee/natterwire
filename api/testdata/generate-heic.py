"""Regenerate synthetic fixtures with Pillow and pillow-heif; not needed for tests."""
from pathlib import Path
from PIL import Image, ImageDraw
import pillow_heif

pillow_heif.register_heif_opener()
image = Image.new("RGB", (2048, 1536), "red")
draw = ImageDraw.Draw(image)
draw.rectangle((1024, 0, 2047, 767), fill="lime")
draw.rectangle((0, 768, 1023, 1535), fill="blue")
draw.rectangle((1024, 768, 2047, 1535), fill="yellow")
for orientation in (2, 6):
    exif = Image.Exif()
    exif[274] = orientation
    # libheif translates the EXIF orientation into HEIF irot/imir properties.
    image.save(Path(__file__).with_name(f"orientation-{orientation}.heic"),
               quality=90, exif=exif.tobytes())
