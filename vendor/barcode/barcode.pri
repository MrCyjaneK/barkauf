# Vendored from https://github.com/monich/sailfish-barcode (MIT).
# Camera frames on this phone are not CPU-readable. Code Reader grabs the
# viewfinder and decodes that image with the bundled ZXing.

DEFINES += NO_ICONV
INCLUDEPATH += $$PWD/zxing $$PWD/scanner $$PWD/include
QT += concurrent
QMAKE_CXXFLAGS += -Wno-unused-parameter -Wno-implicit-fallthrough


SOURCES += \
    $$PWD/scanner/BarcodeImageGrabber.cpp \
    $$PWD/scanner/BarcodeScanner.cpp \
    $$PWD/scanner/Decoder.cpp \
    $$PWD/scanner/ImageSource.cpp

HEADERS += \
    $$PWD/scanner/BarcodeImageGrabber.h \
    $$PWD/scanner/BarcodeScanner.h \
    $$PWD/scanner/Decoder.h \
    $$PWD/scanner/ImageSource.h

# zxing

SOURCES += \
    $$PWD/zxing/bigint/BigIntegerAlgorithms.cc \
    $$PWD/zxing/bigint/BigInteger.cc \
    $$PWD/zxing/bigint/BigIntegerUtils.cc \
    $$PWD/zxing/bigint/BigUnsigned.cc \
    $$PWD/zxing/bigint/BigUnsignedInABase.cc

HEADERS += \
    $$PWD/zxing/bigint/BigIntegerAlgorithms.hh \
    $$PWD/zxing/bigint/BigInteger.hh \
    $$PWD/zxing/bigint/BigIntegerLibrary.hh \
    $$PWD/zxing/bigint/BigIntegerUtils.hh \
    $$PWD/zxing/bigint/BigUnsigned.hh \
    $$PWD/zxing/bigint/BigUnsignedInABase.hh \
    $$PWD/zxing/bigint/NumberlikeArray.hh

SOURCES += \
    $$PWD/zxing/zxing/common/BitArray.cpp \
    $$PWD/zxing/zxing/common/BitMatrix.cpp \
    $$PWD/zxing/zxing/common/BitSource.cpp \
    $$PWD/zxing/zxing/common/CharacterSetECI.cpp \
    $$PWD/zxing/zxing/common/DecoderResult.cpp \
    $$PWD/zxing/zxing/common/DetectorResult.cpp \
    $$PWD/zxing/zxing/common/GlobalHistogramBinarizer.cpp \
    $$PWD/zxing/zxing/common/GridSampler.cpp \
    $$PWD/zxing/zxing/common/HybridBinarizer.cpp \
    $$PWD/zxing/zxing/common/IllegalArgumentException.cpp \
    $$PWD/zxing/zxing/common/PerspectiveTransform.cpp \
    $$PWD/zxing/zxing/common/Str.cpp \
    $$PWD/zxing/zxing/common/StringUtils.cpp

HEADERS += \
    $$PWD/zxing/zxing/common/Array.h \
    $$PWD/zxing/zxing/common/BitArray.h \
    $$PWD/zxing/zxing/common/BitMatrix.h \
    $$PWD/zxing/zxing/common/BitSource.h \
    $$PWD/zxing/zxing/common/CharacterSetECI.h \
    $$PWD/zxing/zxing/common/Counted.h \
    $$PWD/zxing/zxing/common/DecoderResult.h \
    $$PWD/zxing/zxing/common/DetectorResult.h \
    $$PWD/zxing/zxing/common/GlobalHistogramBinarizer.h \
    $$PWD/zxing/zxing/common/GridSampler.h \
    $$PWD/zxing/zxing/common/HybridBinarizer.h \
    $$PWD/zxing/zxing/common/IllegalArgumentException.h \
    $$PWD/zxing/zxing/common/PerspectiveTransform.h \
    $$PWD/zxing/zxing/common/Point.h \
    $$PWD/zxing/zxing/common/Str.h \
    $$PWD/zxing/zxing/common/StringUtils.h \
    $$PWD/zxing/zxing/common/Types.h

SOURCES += \
    $$PWD/zxing/zxing/common/detector/MonochromeRectangleDetector.cpp \
    $$PWD/zxing/zxing/common/detector/WhiteRectangleDetector.cpp

HEADERS += \
    $$PWD/zxing/zxing/common/detector/MathUtils.h \
    $$PWD/zxing/zxing/common/detector/MonochromeRectangleDetector.h \
    $$PWD/zxing/zxing/common/detector/WhiteRectangleDetector.h

SOURCES += \
    $$PWD/zxing/zxing/common/reedsolomon/GenericGF.cpp \
    $$PWD/zxing/zxing/common/reedsolomon/GenericGFPoly.cpp \
    $$PWD/zxing/zxing/common/reedsolomon/ReedSolomonDecoder.cpp \
    $$PWD/zxing/zxing/common/reedsolomon/ReedSolomonException.cpp

HEADERS += \
    $$PWD/zxing/zxing/common/reedsolomon/GenericGF.h \
    $$PWD/zxing/zxing/common/reedsolomon/GenericGFPoly.h \
    $$PWD/zxing/zxing/common/reedsolomon/ReedSolomonDecoder.h \
    $$PWD/zxing/zxing/common/reedsolomon/ReedSolomonException.h

SOURCES += \
    $$PWD/zxing/zxing/BarcodeFormat.cpp \
    $$PWD/zxing/zxing/Binarizer.cpp \
    $$PWD/zxing/zxing/BinaryBitmap.cpp \
    $$PWD/zxing/zxing/ChecksumException.cpp \
    $$PWD/zxing/zxing/DecodeHints.cpp \
    $$PWD/zxing/zxing/EncodeHint.cpp \
    $$PWD/zxing/zxing/Exception.cpp \
    $$PWD/zxing/zxing/FormatException.cpp \
    $$PWD/zxing/zxing/InvertedLuminanceSource.cpp \
    $$PWD/zxing/zxing/LuminanceSource.cpp \
    $$PWD/zxing/zxing/MultiFormatReader.cpp \
    $$PWD/zxing/zxing/Reader.cpp \
    $$PWD/zxing/zxing/Result.cpp \
    $$PWD/zxing/zxing/ResultIO.cpp \
    $$PWD/zxing/zxing/ResultPointCallback.cpp \
    $$PWD/zxing/zxing/ResultPoint.cpp

HEADERS += \
    $$PWD/zxing/zxing/BarcodeFormat.h \
    $$PWD/zxing/zxing/Binarizer.h \
    $$PWD/zxing/zxing/BinaryBitmap.h \
    $$PWD/zxing/zxing/ChecksumException.h \
    $$PWD/zxing/zxing/DecodeHints.h \
    $$PWD/zxing/zxing/EncodeHint.h \
    $$PWD/zxing/zxing/Exception.h \
    $$PWD/zxing/zxing/FormatException.h \
    $$PWD/zxing/zxing/IllegalStateException.h \
    $$PWD/zxing/zxing/InvertedLuminanceSource.h \
    $$PWD/zxing/zxing/LuminanceSource.h \
    $$PWD/zxing/zxing/MultiFormatReader.h \
    $$PWD/zxing/zxing/NotFoundException.h \
    $$PWD/zxing/zxing/ReaderException.h \
    $$PWD/zxing/zxing/Reader.h \
    $$PWD/zxing/zxing/Result.h \
    $$PWD/zxing/zxing/ResultPointCallback.h \
    $$PWD/zxing/zxing/ResultPoint.h \
    $$PWD/zxing/zxing/UnsupportedEncodingException.h \
    $$PWD/zxing/zxing/WriterException.h \
    $$PWD/zxing/zxing/ZXing.h

SOURCES += \
    $$PWD/zxing/zxing/aztec/AztecDetectorResult.cpp \
    $$PWD/zxing/zxing/aztec/AztecReader.cpp \
    $$PWD/zxing/zxing/aztec/decoder/AztecDecoder.cpp \
    $$PWD/zxing/zxing/aztec/detector/AztecDetector.cpp

HEADERS += \
    $$PWD/zxing/zxing/aztec/AztecDetectorResult.h \
    $$PWD/zxing/zxing/aztec/AztecReader.h \
    $$PWD/zxing/zxing/aztec/decoder/Decoder.h \
    $$PWD/zxing/zxing/aztec/detector/Detector.h

SOURCES += \
    $$PWD/zxing/zxing/oned/CodaBarReader.cpp \
    $$PWD/zxing/zxing/oned/Code128Reader.cpp \
    $$PWD/zxing/zxing/oned/Code39Reader.cpp \
    $$PWD/zxing/zxing/oned/Code93Reader.cpp \
    $$PWD/zxing/zxing/oned/EAN13Reader.cpp \
    $$PWD/zxing/zxing/oned/EAN8Reader.cpp \
    $$PWD/zxing/zxing/oned/ITFReader.cpp \
    $$PWD/zxing/zxing/oned/MultiFormatOneDReader.cpp \
    $$PWD/zxing/zxing/oned/MultiFormatUPCEANReader.cpp \
    $$PWD/zxing/zxing/oned/OneDReader.cpp \
    $$PWD/zxing/zxing/oned/OneDResultPoint.cpp \
    $$PWD/zxing/zxing/oned/UPCAReader.cpp \
    $$PWD/zxing/zxing/oned/UPCEANReader.cpp \
    $$PWD/zxing/zxing/oned/UPCEReader.cpp

HEADERS += \
    $$PWD/zxing/zxing/oned/CodaBarReader.h \
    $$PWD/zxing/zxing/oned/Code128Reader.h \
    $$PWD/zxing/zxing/oned/Code39Reader.h \
    $$PWD/zxing/zxing/oned/Code93Reader.h \
    $$PWD/zxing/zxing/oned/EAN13Reader.h \
    $$PWD/zxing/zxing/oned/EAN8Reader.h \
    $$PWD/zxing/zxing/oned/ITFReader.h \
    $$PWD/zxing/zxing/oned/MultiFormatOneDReader.h \
    $$PWD/zxing/zxing/oned/MultiFormatUPCEANReader.h \
    $$PWD/zxing/zxing/oned/OneDReader.h \
    $$PWD/zxing/zxing/oned/OneDResultPoint.h \
    $$PWD/zxing/zxing/oned/UPCAReader.h \
    $$PWD/zxing/zxing/oned/UPCEANReader.h \
    $$PWD/zxing/zxing/oned/UPCEReader.h

SOURCES += \
    $$PWD/zxing/zxing/pdf417/PDF417Reader.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ErrorCorrection.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ModulusGF.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ModulusPoly.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/PDF417BitMatrixParser.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/PDF417DecodedBitStreamParser.cpp \
    $$PWD/zxing/zxing/pdf417/decoder/PDF417Decoder.cpp \
    $$PWD/zxing/zxing/pdf417/detector/LinesSampler.cpp \
    $$PWD/zxing/zxing/pdf417/detector/PDF417Detector.cpp

HEADERS += \
    $$PWD/zxing/zxing/pdf417/PDF417Reader.h \
    $$PWD/zxing/zxing/pdf417/decoder/BitMatrixParser.h \
    $$PWD/zxing/zxing/pdf417/decoder/DecodedBitStreamParser.h \
    $$PWD/zxing/zxing/pdf417/decoder/Decoder.h \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ErrorCorrection.h \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ModulusGF.h \
    $$PWD/zxing/zxing/pdf417/decoder/ec/ModulusPoly.h \
    $$PWD/zxing/zxing/pdf417/detector/Detector.h \
    $$PWD/zxing/zxing/pdf417/detector/LinesSampler.h

SOURCES += \
    $$PWD/zxing/zxing/qrcode/QRCodeReader.cpp \
    $$PWD/zxing/zxing/qrcode/QRErrorCorrectionLevel.cpp \
    $$PWD/zxing/zxing/qrcode/QRFormatInformation.cpp \
    $$PWD/zxing/zxing/qrcode/QRVersion.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRBitMatrixParser.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRDataBlock.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRDataMask.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRDecodedBitStreamParser.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRDecoder.cpp \
    $$PWD/zxing/zxing/qrcode/decoder/QRMode.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRAlignmentPattern.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRAlignmentPatternFinder.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRDetector.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRFinderPattern.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRFinderPatternFinder.cpp \
    $$PWD/zxing/zxing/qrcode/detector/QRFinderPatternInfo.cpp

HEADERS += \
    $$PWD/zxing/zxing/qrcode/decoder/BitMatrixParser.h \
    $$PWD/zxing/zxing/qrcode/decoder/DataBlock.h \
    $$PWD/zxing/zxing/qrcode/decoder/DataMask.h \
    $$PWD/zxing/zxing/qrcode/decoder/DecodedBitStreamParser.h \
    $$PWD/zxing/zxing/qrcode/decoder/Decoder.h \
    $$PWD/zxing/zxing/qrcode/decoder/Mode.h \
    $$PWD/zxing/zxing/qrcode/detector/AlignmentPatternFinder.h \
    $$PWD/zxing/zxing/qrcode/detector/AlignmentPattern.h \
    $$PWD/zxing/zxing/qrcode/detector/Detector.h \
    $$PWD/zxing/zxing/qrcode/detector/FinderPatternFinder.h \
    $$PWD/zxing/zxing/qrcode/detector/FinderPattern.h \
    $$PWD/zxing/zxing/qrcode/detector/FinderPatternInfo.h \
    $$PWD/zxing/zxing/qrcode/ErrorCorrectionLevel.h \
    $$PWD/zxing/zxing/qrcode/FormatInformation.h \
    $$PWD/zxing/zxing/qrcode/QRCodeReader.h \
    $$PWD/zxing/zxing/qrcode/Version.h

SOURCES += \
    $$PWD/zxing/zxing/datamatrix/DataMatrixReader.cpp \
    $$PWD/zxing/zxing/datamatrix/DataMatrixVersion.cpp \
    $$PWD/zxing/zxing/datamatrix/decoder/DataMatrixBitMatrixParser.cpp \
    $$PWD/zxing/zxing/datamatrix/decoder/DataMatrixDataBlock.cpp \
    $$PWD/zxing/zxing/datamatrix/decoder/DataMatrixDecodedBitStreamParser.cpp \
    $$PWD/zxing/zxing/datamatrix/decoder/DataMatrixDecoder.cpp \
    $$PWD/zxing/zxing/datamatrix/detector/DataMatrixCornerPoint.cpp \
    $$PWD/zxing/zxing/datamatrix/detector/DataMatrixDetector.cpp \
    $$PWD/zxing/zxing/datamatrix/detector/DataMatrixDetectorException.cpp

HEADERS += \
    $$PWD/zxing/zxing/datamatrix/DataMatrixReader.h \
    $$PWD/zxing/zxing/datamatrix/decoder/BitMatrixParser.h \
    $$PWD/zxing/zxing/datamatrix/decoder/DataBlock.h \
    $$PWD/zxing/zxing/datamatrix/decoder/DecodedBitStreamParser.h \
    $$PWD/zxing/zxing/datamatrix/decoder/Decoder.h \
    $$PWD/zxing/zxing/datamatrix/detector/CornerPoint.h \
    $$PWD/zxing/zxing/datamatrix/detector/DetectorException.h \
    $$PWD/zxing/zxing/datamatrix/detector/Detector.h \
    $$PWD/zxing/zxing/datamatrix/Version.h

