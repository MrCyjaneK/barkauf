# Barkauf

> Everything needs Bitcoin, not only finance.

## What and why?

SailfishOS app, the first Bitcoin wallet for the system to support Lightning, built entirely from scratch using the native SailfishOS SDK. Supports onchain, ark, lightning, scanning, NFC tap to pay, notifications, mempool accelerator for stuck transactions, VTXOs refresh, and a native user interface that features almost all the most important features of a Bitcoin wallet.

Payments are already gatekept heavily; you can't hook into Google Wallet and add a card that speaks Bitcoin over NFC and work with the magic "tap to pay". There are thousands of different things you can't do; some of them you don't even notice. Ever noticed how the Clock app on iPhone shows the current time? That's a private API. Camera app? It works because there is an if statement in the kernel that gives it special memory permission. AI integrations in Xcode? First-party extension - you can't create your own. Web browser? Sorry, only Apple can make a web browser engine.

We don't own our technology. We owned them years ago, but now we don't. Sure, we can buy a ThinkPad and run only free and open-source software on it, but how many people will go out of their way to bring a laptop to pay for groceries?

Once sideloading apps is locked only for developers who pay, complete KYC, and are approved by Google - which is the last step - we already lost the bootloader unlock and essentially any permissions on our "own" devices; we will be one terms-of-service change away from self-custodial wallets getting banned. And Bitcoin falls under so many different "categories".

It is P2P, it is 3rd party content, it is contacting custom servers, it is running in the background, it is money - all and any of these could be banned or require extra regulation to stay in the app store.

That's why we need to fight on the other end too, not only for open finance but also for open technology in general, as one can't work without the other, and both of them work in favor of us - humans.

That's why I've selected a rather niche target: to offer open finance on an open platform for people who want that.

Everything needs a reform similar to the one that Bitcoin brought to the financial system. Everything needs to be more open - and no one will do that if we won't do that ourselves.

## Why the long version?

Bitcoin is great. Bitcoin is open. I think not a single person dares to disagree. We may have different opinions, use different protocols; we can be right, and someone can be wrong, but the universal truth is that Bitcoin is for everyone.

Yesterday (01.10.2026) we heard about how great Bitcoin is when compared to Visa… and truly this is all great.

Bitcoin is also small. We run on platforms that could swallow our entire market cap. Apple is worth US$5T, Google is US$4T, Microsoft is US$3.8T, and Bitcoin's market cap is US$1.5T. And this is okay; they have been around for much longer, but the point is they don’t do business on crypto. Usually.

Samsung, in their keynote, announced the addition of USDC to the Samsung wallet. There aren’t necessarily many details available; we don’t know how open the implementation will be, whether it will be self-custodial, or whether it supports Bitcoin - we have no details. I’d argue this is not the adoption we want.

Why I mention this is that once the gatekeepers start competing with us directly when it comes to implementations, we are starting from the losing position.

10 Years ago, when you bought a phone, you could root it, jailbreak it, put custom firmware on it, and treat it as yours. There were no limits.

5 Years ago, you could root it, jailbreak it if you had an older iPhone, but you could sideload external .apk files fairly easily

Now? You can’t unlock the bootloader on the phone you buy, the modding scene is not in a great condition, and Google is actively trying to make sideloading apps more difficult than Apple.

It’s a phrase I heard like 500 times at this and other conferences. It’s boiling the frog alive. With fully unlocked software, you could customize your device, block ads, and remove spyware. Software got better; you can’t do that, but there’s a better way to customize it, so I guess it’s fine.

Sure, I can’t root my phone to mod the YouTube app, but I can still sideload a modded version. Well, until September 30, 2026, that was true. Google started rolling out the update in Asia and will finish the rollout worldwide in 2027.

Will this affect you? No. Not now. It is just another small change. A minor inconvenience. US$25 to develop apps for Android. It’s one-time! Apple requires US$100 a year! We are the open platform.

And sure, they probably won’t ban any Bitcoin wallet. There’s plenty on the Play Store already. They probably won’t revoke your account even for making something that is not “Play Store” safe. But are you sure that they won’t revoke the signing certificate that distributes the ReVanced patcher that modifies YouTube and actively harms Google’s revenue?

If Google follows Samsung and integrates Bitcoin payments into Google Wallet, will they consider other apps “inconvenient”? They sure won’t declare cryptocurrencies banned, though I wouldn’t be surprised if that would happen. But will they want more competition on the market? The slippery slope is real.

1. You can do anything on your phone.
2. You don’t have to anymore because the cool stuff comes preinstalled!
3. You can install anything on your phone.
4. You can sideload apps to your phone.
5. You can sideload apps from 3rd-party stores.
6. You just need to pay US$25 to distribute the app outside of the app store.
7. You just need to wait 24 hours before sideloading is enabled.
8. You just need to agree to these terms.
9. You don’t need to install the app because there’s a similar one preinstalled.
10. You just need a cryptocurrency license to continue distributing your app.
11. Sorry, the sync is not using our SDK and edge locations; therefore, we will mark your app with a warning because it drains battery and is slow.
12. Sorry, the P2P protocol you engage in is not routed properly; we will mark your app as dangerous.
13. Your app connects in the background to 3rd party servers instead of being notified by Firebase - you need to fix that before distributing your app.
14. Dangerous apps are harmful.
15. Signing certificates won’t be issued for malware.
16. All harmful software is malware.

Sure, you can sideload the software; you can develop anything, and not a single step here is harmful. Hence, dare I say every single step here is a sane decision, to protect you, to improve service, to enforce terms, to protect the children. Explaining why something is bad takes ages, involves boring words, and is genuinely difficult to do. They know what they are doing.

Software that runs in the background? Harmful to the environment, low quality, malware.

Chip that the movie industry put in every single piece of hardware to ensure you can’t copy a movie you legally own? “Digital rights management”. The audience falls asleep in the middle of the name because of how boring it sounds, and anyway, if it manages my rights, that’s good. I like my rights.

And for the app? It’s a statement. It’s an option. It’s an alternative. Built for open devices, running open software. For people who are fed up with the system, the technological one, not the financial one.

In order to get an open financial system, we need to live in an open technological world… and we kind of do, but the companies and governments are starting to make it less and less open, and in order to win, we need to fight both.

Notes

External dependencies used in this product are:

* Code Reader camera grabber and ZXing, vendored under vendor/barcode
* sfosbuild - build tooling I can use instead of the official Sailfish IDE
* Noah wallet was used as UI reference; the code was of very limited usefulness because of an entirely different tech stack and was entirely unused during the development process.

None of these have a direct impact nor are necessary, but since they are clearly code written before the hackathon, they are disclosed per hackathon rules.

Barkauf is a wannabe port of Noah wallet to SailfishOS - reimplemented from scratch using nothing but the UI for “flow reference”. All code written in this repository was written starting Thursday, October 1st, 5 p.m. or so.