"""Build the standalone Jeedom patch archive from its verified distribution files."""
import argparse
from datetime import datetime, timezone
import hashlib
from pathlib import Path, PurePosixPath
import re
import zipfile


def package(root, version, timestamp, output):
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?', version):
        raise ValueError('Expected a semantic release version without the v prefix')
    bundle = root / 'Jeedom'
    checksums = bundle / 'SHA256SUMS'
    if bundle.is_symlink() or not bundle.is_dir() or not bundle.resolve().is_relative_to(root.resolve()):
        raise ValueError('Unsafe bundle directory')
    if checksums.is_symlink() or not checksums.is_file() or not checksums.resolve().is_relative_to(bundle.resolve()):
        raise ValueError('Unsafe checksum manifest')
    files = {}
    for line in checksums.read_text(encoding='utf-8').splitlines():
        digest, relative = line.split('  ', 1)
        path = PurePosixPath(relative)
        if not re.fullmatch('[0-9a-f]{64}', digest) or path.is_absolute() or '..' in path.parts or '\\' in relative:
            raise ValueError('Invalid checksum entry')
        source = bundle.joinpath(*path.parts)
        if source.is_symlink() or not source.resolve().is_relative_to(bundle.resolve()) or relative in files:
            raise ValueError('Unsafe or duplicate distribution path: ' + relative)
        content = source.read_bytes()
        if hashlib.sha256(content).hexdigest() != digest:
            raise ValueError('Checksum mismatch: ' + relative)
        files[relative] = content
    if not files:
        raise ValueError('Empty checksum manifest')
    expected = set(files) | {'SHA256SUMS'}
    actual = {path.relative_to(bundle).as_posix() for path in bundle.rglob('*') if path.is_file()}
    if actual != expected:
        raise ValueError(f'Unlisted or missing distribution files: {sorted(actual ^ expected)}')
    files['SHA256SUMS'] = checksums.read_bytes()
    date = datetime.fromtimestamp(timestamp, timezone.utc)
    if not 1980 <= date.year <= 2107:
        raise ValueError('Timestamp is outside the ZIP format range')
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f'AjaxBridge_{version}_jeedom_patch.zip'
    with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as stream:
        for name, content in sorted(files.items()):
            info = zipfile.ZipInfo('Jeedom/' + name, date.timetuple()[:6])
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            stream.writestr(info, content, compresslevel=9)
    with zipfile.ZipFile(archive) as stream:
        if stream.testzip() is not None or len(stream.namelist()) != len(files):
            raise ValueError('ZIP integrity check failed')
        for name, content in files.items():
            if stream.read('Jeedom/' + name) != content:
                raise ValueError('ZIP readback differs: ' + name)
    print(f'{archive.name}: {len(files)} verified files; sha256={hashlib.sha256(archive.read_bytes()).hexdigest()}')
    return archive


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--timestamp', type=int, required=True)
    parser.add_argument('--output', type=Path, default=Path('.'))
    args = parser.parse_args()
    package(Path(__file__).resolve().parents[1], args.version, args.timestamp, args.output)
