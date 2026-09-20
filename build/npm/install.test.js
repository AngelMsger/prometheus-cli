'use strict';

const path = require('path');
const test = require('node:test');
const assert = require('node:assert/strict');

const { assetName, binPath, welcomeText } = require('./install.js');

test('selects Windows release assets for both supported architectures', () => {
  assert.equal(assetName('win32', 'x64'), 'prometheus-cli-windows-amd64.exe');
  assert.equal(assetName('win32', 'arm64'), 'prometheus-cli-windows-arm64.exe');
});

test('uses an exe launcher target on Windows', () => {
  assert.equal(path.basename(binPath('win32').file), 'prometheus-cli.exe');
});

test('rejects unsupported Windows architectures', () => {
  assert.throws(() => assetName('win32', 'ia32'), /unsupported platform win32\/ia32/);
});

test('welcome text explains Skill refresh', () => {
  assert.match(welcomeText(), /prometheus-cli skill install/);
  assert.match(welcomeText(), /reload your agent context/);
});

test('welcome text recommends Prometheus commands', () => {
  assert.match(welcomeText(), /prometheus-cli query instant/);
  assert.match(welcomeText(), /prometheus-cli query range/);
  assert.doesNotMatch(welcomeText(), /prometheus-cli (job|build|stream|search)/);
});
