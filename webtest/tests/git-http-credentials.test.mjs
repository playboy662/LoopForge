import test from 'node:test';
import assert from 'node:assert/strict';
import { loadApp, settle } from '../lib/load-app.mjs';

test('Git HTTPS credentials are managed per host with write-only tokens and verification', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  const endpoint = '/api/users/1/git-http-credentials';
  let creds = [
    { id: 'gh', name: 'GitHub', platform: 'github', host: 'github.com', username: 'x-access-token' },
    { id: 'managed-internal', name: '内部托管凭据', platform: 'gitlab', host: 'git.internal.example', username: 'oauth2', managed: true },
  ];
  app.router.get(endpoint, () => creds);
  app.router.put(endpoint, ({ body }) => {
    const cred = { id: body.id || 'corp', name: body.name, platform: body.platform, host: body.host, username: body.username };
    creds = [...creds.filter(item => item.id !== cred.id), cred];
    return creds;
  });
  app.router.delete(`${endpoint}/corp`, () => {
    creds = creds.filter(item => item.id !== 'corp');
    return creds;
  });
  app.router.post(`${endpoint}/gh/verify`, { valid: true, login: 'octo' });
  app.evaluate(`
    state.currentUser={...state.data.users[0],id:1,isGuest:false,gitHttpCredentialsConfigured:true};
    state.data.users=[state.currentUser];
    renderPersonalSettings();
    openCredentialWizard();
  `);
  const wizard = app.document.querySelector('#credentialWizard');
  const option = wizard.querySelector('[data-wizard-kind="gitHttpCredentials"]');
  assert.equal(option.disabled, false, 'more hosts can be added after the first credential');
  option.click();
  wizard.querySelector('[data-wizard-next]').click();
  await settle(6);

  const dialog = app.document.querySelector('#gitHTTPCredentialDialog');
  const form = dialog.querySelector('form');
  assert.equal(form.elements.exposeToWorkspace, undefined, 'workspace credentials are always available');
  assert.match(dialog.textContent, /无法从 Token 推导/);
  assert.equal(dialog.querySelectorAll('.git-ssh-key-row').length, 2);
  assert.equal(dialog.querySelector('[data-http-cred-edit="managed-internal"]'), null, 'the managed credential is read-only');
  assert.equal(dialog.querySelector('[data-http-cred-delete="managed-internal"]'), null);

  dialog.querySelector('[data-http-cred-verify="gh"]').click();
  await settle(6);
  assert.match(dialog.querySelector('.git-ssh-status').textContent, /octo/);

  dialog.querySelector('[data-http-cred-add]').click();
  assert.equal(form.elements.exposeToWorkspace, undefined);
  form.elements.host.value = 'https://gitlab.corp.example/';
  form.elements.host.dispatchEvent(new app.window.Event('change', { bubbles: true }));
  assert.equal(form.elements.platform.value, 'gitlab');
  assert.equal(form.elements.name.value, '');
  assert.equal(form.elements.name.placeholder, 'gitlab.corp.example');
  form.elements.token.value = 'glpat-test-only';
  form.dispatchEvent(new app.window.Event('submit', { bubbles: true, cancelable: true }));
  await settle(6);
  const saves = () => app.router.calls.filter(call => call.method === 'PUT' && call.path === endpoint);
  assert.equal(app.evaluate('credentialUser().gitHubCredentialConfigured'), true, 'saving the host list refreshes the GitHub authorization row');
  assert.deepEqual(saves()[0].body, {
    host: 'https://gitlab.corp.example/', platform: 'gitlab', name: 'gitlab.corp.example',
    username: 'oauth2', token: 'glpat-test-only',
  });
  assert.equal(form.hidden, true);
  assert.equal(form.elements.token.value, '', 'saving clears the token input');
  assert.doesNotMatch(dialog.querySelector('.git-ssh-key-list').textContent, /仅控制面/);

  dialog.querySelector('[data-http-cred-edit="gh"]').click();
  assert.equal(form.elements.token.value, '', 'editing never reads back the stored token');
  assert.equal(form.elements.token.required, false);
  assert.equal(form.elements.host.readOnly, true);
  form.elements.name.value = 'GitHub personal';
  form.dispatchEvent(new app.window.Event('submit', { bubbles: true, cancelable: true }));
  await settle(6);
  assert.deepEqual(saves()[1].body, {
    id: 'gh', host: 'github.com', platform: 'github', name: 'GitHub personal',
    username: 'x-access-token', token: '',
  });

  dialog.querySelector('[data-http-cred-delete="corp"]').click();
  assert.equal(app.router.calls.some(call => call.method === 'DELETE'), false);
  dialog.querySelector('[data-http-cred-remove="corp"]').click();
  await settle(6);
  assert.equal(dialog.querySelectorAll('.git-ssh-key-row').length, 2);
  dialog.querySelector('[data-http-cred-close]').click();
  assert.equal(app.document.querySelector('#gitHTTPCredentialDialog'), null);
  assert.equal(app.evaluate('state.currentUser.gitHttpCredentialsConfigured'), true);
});

test('Git HTTP is added from the menu and managed per saved host', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  const endpoint = '/api/users/1/git-http-credentials';
  const secure = { id: 'secure', name: 'Secure Git', platform: 'gitlab', host: 'git.example.org' };
  const plain = { id: 'plain', name: 'Plain Git', platform: 'gitlab', host: 'http://192.0.2.10:9433' };
  let credentials = [secure];
  app.router.get(endpoint, () => credentials);
  app.router.put(endpoint, ({ body }) => {
    credentials = [secure, { ...plain, name: body.name, host: body.host, username: body.username }];
    return credentials;
  });
  app.router.delete(`${endpoint}/plain`, () => {
    credentials = [secure];
    return credentials;
  });
  app.evaluate(`
    state.currentUser={...state.data.users[0],id:1,isGuest:false,gitHttpCredentialsConfigured:true};
    state.data.users=[state.currentUser];
    renderPersonalSettings();
  `);
  await settle(6);

  const rows = () => app.document.querySelector('[data-credential-git-rows]');
  assert.doesNotMatch(rows().textContent, /Git HTTP/);
  assert.equal(app.document.querySelector('[data-credential-row="gitPlainHttpCredentials"]'), null);
  const menu = app.document.querySelector('[data-credential-add-menu="platform"]');
  const option = menu.querySelector('option[value="gitPlainHttpCredentials"]');
  assert.ok(option);
  option.selected = true;
  menu.dispatchEvent(new app.window.Event('change', { bubbles: true }));
  await settle(6);
  const activeDialog = () => app.document.querySelector('#gitHTTPCredentialDialog');
  let dialog = activeDialog();
  const form = dialog.querySelector('form');
  assert.match(dialog.textContent, /Git HTTP 访问令牌/);
  assert.equal(form.hidden, false);
  form.elements.host.value = plain.host;
  form.elements.name.value = plain.name;
  form.elements.token.value = 'git-http-test-token';
  form.dispatchEvent(new app.window.Event('submit', { bubbles: true, cancelable: true }));
  await settle(6);
  dialog.querySelector('[data-http-cred-close]').click();
  const edit = rows().querySelector('[data-credential-git-id="plain"]');
  assert.match(edit.closest('.credential-row').textContent, /Plain Git\s*Git HTTP · http:\/\/192\.0\.2\.10:9433/);
  edit.click();
  await settle(6);
  dialog = activeDialog();
  assert.equal(dialog.querySelector('form').elements.host.value, plain.host);
  assert.equal(dialog.querySelector('form').elements.host.readOnly, true);
  assert.equal(dialog.querySelector('form').elements.token.value, '');
  assert.doesNotMatch(dialog.textContent, /Secure Git/);
  dialog.querySelector('[data-http-cred-delete="plain"]').click();
  dialog.querySelector('[data-http-cred-remove="plain"]').click();
  await settle(6);
  assert.equal(rows().querySelector('[data-credential-git-id="plain"]'), null);
  assert.ok(rows().querySelector('[data-credential-git-id="secure"]'));
  assert.equal(app.document.querySelector('[data-credential-row="gitPlainHttpCredentials"]'), null);
  assert.equal(app.evaluate(`parseGongfengRepoURL('http://192.0.2.10:9433/team/code')?.cloneUrl`),
    'http://192.0.2.10:9433/team/code');
  assert.equal(app.evaluate(`parseGongfengRepoURL('http://git.example.com/team/code.git')`), null);
  assert.equal(app.evaluate(`parseGongfengRepoURL('http://192.0.2.10:9433/team/../code.git')`), null);
});
