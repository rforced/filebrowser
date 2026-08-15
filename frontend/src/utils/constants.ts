const name: string = window.FileBrowser.Name || "File Browser";
const disableExternal: boolean = window.FileBrowser.DisableExternal;
const disableUsedPercentage: boolean = window.FileBrowser.DisableUsedPercentage;
const baseURL: string = window.FileBrowser.BaseURL;
const staticURL: string = window.FileBrowser.StaticURL;
const recaptcha: string = window.FileBrowser.ReCaptcha;
const recaptchaKey: string = window.FileBrowser.ReCaptchaKey;
const version: string = window.FileBrowser.Version;
const logoURL = `${staticURL}/img/logo.svg`;
const authMethod = window.FileBrowser.AuthMethod;
const logoutPage: string = window.FileBrowser.LogoutPage;
const theme: UserTheme = window.FileBrowser.Theme;
const enableThumbs: boolean = window.FileBrowser.EnableThumbs;
const resizePreview: boolean = window.FileBrowser.ResizePreview;
const tusSettings = window.FileBrowser.TusSettings;
const origin = window.location.origin;
const tusEndpoint = `/api/tus`;
const hideLoginButton = window.FileBrowser.HideLoginButton;
const domain: string = window.FileBrowser.Domain;
const teamId: string = window.FileBrowser.TeamID;
const filesystemId: string = window.FileBrowser.FilesystemID;
const cspNonce: string = window.FileBrowser.Nonce ?? "";

export {
  name,
  disableExternal,
  disableUsedPercentage,
  baseURL,
  logoURL,
  recaptcha,
  recaptchaKey,
  version,
  authMethod,
  logoutPage,
  theme,
  enableThumbs,
  resizePreview,
  tusSettings,
  origin,
  tusEndpoint,
  hideLoginButton,
  domain,
  teamId,
  filesystemId,
  cspNonce,
};
