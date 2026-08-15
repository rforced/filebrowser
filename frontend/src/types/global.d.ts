export {};

declare global {
  interface Window {
    FileBrowser: any;
    grecaptcha: any;
    __prependStaticUrl: (url: string) => string;
  }

  interface HTMLElement {
    __vue__: any;
  }

  interface HTMLElement {
    clickOutsideEvent?: (event: Event) => void;
  }
}
